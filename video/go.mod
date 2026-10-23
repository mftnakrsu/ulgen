// Not Go code. This file only keeps the Go tool of the repository out of video/: after
// npm install, node_modules holds a .go file of a dependency (flatted), and "./..." in the
// ulgen module stops at a directory that has a go.mod of its own.
module github.com/mftnakrsu/ulgen/video

go 1.26.0
