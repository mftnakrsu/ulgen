// Package jsonc edits JSON files that may carry comments and trailing commas
// (the format Qwen Code and opencode use for their settings). The files
// belong to the user, so every edit keeps the promises below. Each holds
// for any text that parses, and the tests check each one.
//
//   - G1. The result parses. The text between JSON tokens is only ever cut
//     between a comment and what stands next to it, never inside a comment.
//     On top of that, Set and UpdateArray pack and parse their result
//     before the document takes it over; a result that fails is dropped
//     and an error returned.
//   - G2. The edit means exactly what was asked: the document decodes to
//     what it decoded to before with the requested change and nothing else.
//     The check of G1 enforces this as well.
//   - G3. No comment is lost or repeated. Comments inside a value that is
//     replaced move in front of its member or element; comments next to or
//     inside an element that is removed stay where it was. The check of G1
//     enforces this as well.
//   - G4. No trailing comma is introduced. An object or array ends with a
//     comma afterwards if and only if it did before; new ones have none,
//     and an array left empty cannot have one.
//   - G5. Nothing changes where nothing was asked. Writing a value equal
//     to the one already there changes no byte, so a second identical run
//     leaves a file as it is. Around a value that is written the text stays
//     as it is. A new member or element only adds text: a comma, a line
//     break, itself. (One exception: an object or array with nothing but
//     spaces between its brackets is laid out like an empty one, and those
//     spaces go when it opens up.) An element that is removed takes its
//     comma and the spaces and line break of its own line with it, and an
//     array left without elements and comments is written "[]".
//   - G6. Line breaks. A document's line break is that of its first line
//     (LF without one). Every line break the package writes is that one,
//     and no existing one is changed. The line break and the indentation
//     are read from the text as it is when an edit starts, so two edits on
//     one document give what the second gives on the text the first left.
//   - G7. A key that occurs more than once in its object is refused when it
//     is on the path of an edit, not guessed at. Get reads the last one, as
//     JSON loaders do.
//   - G8. A leading byte-order mark is accepted and written back.
//   - G9. A text with nothing but whitespace and comments is a document
//     without a value. The first Set puts an object behind the text.
//   - G10. A removal never creates anything.
//   - G11. A text may end inside a line comment, with no line break behind
//     it. It parses, Bytes returns it byte for byte, and after an edit the
//     comment is still the end of the text. Only when something is put
//     behind the comment does it get the document's line break.
//   - G12. A value with a repeated key is refused. Set and UpdateArray
//     return an error and change nothing when a value they are to write
//     holds an object with the same key twice, at any depth.
//
// The package builds on hujson's syntax tree, which reproduces a text byte
// for byte. It does not use hujson's Patch, which puts a new member on the
// line of the one before it, nor Format, which rewrites the whole file and
// adds trailing commas that Qwen Code rejects.
package jsonc
