package main

// os.Root confines reparse points; the opened handle's identity is checked
// against Lstat before any data is read. Windows has no Unix FIFO open flags.
const mediaOpenFlags = 0
