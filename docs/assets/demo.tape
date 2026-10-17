# The recording in the README. To make it again: a folder /tmp/ulgen-demo/hello
# with a small main.go in it, ulgen on PATH and signed in, Claude Code
# installed, and a terminal that was not started from inside Claude Code.
Output docs/assets/demo.gif
Set Shell bash
Set FontSize 17
Set Width 1180
Set Height 640
Set Padding 24
Set Theme "GitHub Dark"
Set TypingSpeed 45ms
Set PlaybackSpeed 1.6

# Off camera, and only while this folder is not trusted yet. The dialog opens
# on "No, exit"; Down and Enter choose "Yes, I trust this folder". Theme and
# the security notes are already answered in CLAUDE_CONFIG_DIR. /exit returns to
# the shell, so the recording starts at the prompt.
Hide
Type "cd /tmp/ulgen-demo/hello && export PS1='$ ' LANG=en_US.UTF-8 CLAUDE_CONFIG_DIR=/tmp/ulgen-demo/cfg && clear"
Enter
Sleep 1s
Type "ulgen claude"
Enter
Sleep 6s
Down
Sleep 500ms
Enter
Sleep 5s
Type "/exit"
Sleep 500ms
Enter
Sleep 2s
Type "clear"
Enter
Sleep 1s
Show

Type "ulgen doctor"
Enter
Sleep 5s

Type "ulgen status"
Enter
Sleep 6s

Type "clear"
Enter
Type "ulgen claude"
Enter
Sleep 4s
Type "What does main.go print?"
Sleep 500ms
Enter
Wait+Screen@90s /6765/
Sleep 4s
