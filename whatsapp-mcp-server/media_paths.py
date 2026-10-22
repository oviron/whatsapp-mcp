import os
import stat
import tempfile
from contextlib import contextmanager
from pathlib import Path

MAX_MEDIA_BYTES = 100 * 1024 * 1024
STORE_DIR = Path(__file__).resolve().parent.parent / "whatsapp-bridge" / "store"


class MediaPathError(ValueError):
    pass


def _relative(root, path):
    try:
        return path.relative_to(root).parts
    except ValueError:
        return None


def _blocked(path, home, store):
    in_store = _relative(store, path)
    if in_store is not None and len(in_store) < 2:
        return True
    in_home = _relative(home, path)
    # Dotfiles hold credentials; Library (macOS) and AppData (Windows) hold
    # keychains, browser cookies and other apps' message databases.
    return bool(in_home and (in_home[0].startswith(".") or in_home[0] in ("Library", "AppData"))
                and in_store is None)


def _checked_path(path):
    original = Path(os.path.abspath(path))
    home = Path.home().resolve(strict=True)
    store = STORE_DIR.resolve()
    resolved = original.resolve(strict=True)
    if _blocked(original, home, STORE_DIR) or _blocked(resolved, home, store):
        raise MediaPathError("Media path is not allowed (WHATSAPP_MEDIA_DIRS)")
    configured = os.environ.get("WHATSAPP_MEDIA_DIRS", "")
    roots = configured.split(os.pathsep) if configured else [home, tempfile.gettempdir(), store]
    if not any(_relative(Path(root).resolve(), resolved) for root in roots if root):
        raise MediaPathError("Media path is not allowed (WHATSAPP_MEDIA_DIRS)")
    return resolved


def _windows_handle_path(fd):
    import ctypes
    import msvcrt
    from ctypes import wintypes

    final_path = ctypes.WinDLL("kernel32", use_last_error=True).GetFinalPathNameByHandleW
    final_path.argtypes = [wintypes.HANDLE, wintypes.LPWSTR, wintypes.DWORD, wintypes.DWORD]
    final_path.restype = wintypes.DWORD
    handle = msvcrt.get_osfhandle(fd)
    size = final_path(handle, None, 0, 0)
    if not size:
        raise OSError("Cannot resolve media handle")
    buffer = ctypes.create_unicode_buffer(size + 1)
    length = final_path(handle, buffer, len(buffer), 0)
    if not length or length >= len(buffer):
        raise OSError("Cannot resolve media handle")
    path = buffer.value
    if path.startswith("\\\\?\\UNC\\"):
        return "\\\\" + path[8:]
    return path.removeprefix("\\\\?\\")


def _open_resolved(resolved):
    if os.name == "nt":
        return os.open(resolved, os.O_RDONLY | os.O_BINARY)
    # Walk without following links after resolution, so replacement cannot
    # redirect the checked source into a hidden directory or a session DB.
    directory = os.open(resolved.anchor, os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in resolved.parts[1:-1]:
            next_dir = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                               dir_fd=directory)
            os.close(directory)
            directory = next_dir
        return os.open(resolved.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK,
                       dir_fd=directory)
    finally:
        os.close(directory)


@contextmanager
def open_media(path):
    try:
        resolved = _checked_path(path)
        with os.fdopen(_open_resolved(resolved), "rb") as source:
            info = os.fstat(source.fileno())
            if not stat.S_ISREG(info.st_mode):
                raise MediaPathError("Media must be a regular file")
            if info.st_size > MAX_MEDIA_BYTES:
                raise MediaPathError("Media exceeds the 100 MiB size limit")
            if os.name == "nt":
                _checked_path(_windows_handle_path(source.fileno()))
            yield source
    except (OSError, RuntimeError, ValueError) as error:
        if isinstance(error, MediaPathError):
            raise
        raise MediaPathError("Media file unavailable or outside allowed directories (WHATSAPP_MEDIA_DIRS)") from None


@contextmanager
def media_snapshot(path):
    with open_media(path) as source, tempfile.TemporaryDirectory(prefix="wa_media_") as directory:
        data = source.read(MAX_MEDIA_BYTES + 1)
        if len(data) > MAX_MEDIA_BYTES:
            raise MediaPathError("Media exceeds the 100 MiB size limit")
        snapshot = Path(directory) / ("source" + Path(path).suffix)
        snapshot.write_bytes(data)
        yield str(snapshot)
