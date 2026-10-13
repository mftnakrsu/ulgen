Output docs/assets/demo.gif
Set Shell bash
Set FontSize 17
Set Width 1180
Set Height 640
Set Padding 24
Set Theme "GitHub Dark"
Set TypingSpeed 45ms
Set PlaybackSpeed 1.6

Hide
Type "cd /tmp/ulgen-demo/hello && export PS1='$ ' LANG=en_US.UTF-8 CLAUDE_CONFIG_DIR=/tmp/ulgen-demo/cfg CLAUDE_CODE_MAX_CONTEXT_TOKENS=1048576 && clear"
Enter
Sleep 1s
Show

Type "ulgen doctor"
Enter
Sleep 6s

Type "ulgen status"
Enter
Sleep 7s

Type "clear"
Enter
Type "cat main.go"
Enter
Sleep 3s

Type `ulgen claude -p "What does main.go print? One sentence." --allowedTools Read`
Enter
Wait+Line@120s /^\$\s*$/
Sleep 5s
