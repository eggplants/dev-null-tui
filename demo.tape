Output demo.mp4

Require go

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 640
Set Padding 10
Set TypingSpeed 75ms
Set Framerate 20

# Everything runs against demo/server, a stand-in for dev-null.sh with made-up people and commits.
Hide
Type "export XDG_CONFIG_HOME=$(mktemp -d) PS1='> ' && go build -o dn ./cmd/dn && go build -o demo-server ./demo/server && (./demo-server &) && until [ -f $XDG_CONFIG_HOME/devnull-tui/config.json ]; do sleep 0.1; done && export PATH=$PWD:$PATH && clear"
Enter
Wait@60s
Show

Type "dn"
Sleep 300ms
Enter
Sleep 2s

# Move through the log, then open a thread.
Type "j"
Sleep 700ms
Type "j"
Sleep 700ms
Type "j"
Sleep 1s
Type "gg"
Sleep 700ms
Enter
Sleep 1.5s

# Star it and reply.
Type "s"
Sleep 1s
Type "r"
Sleep 700ms
Type "Trying it now, it's fast!"
Sleep 500ms
Ctrl+S
Sleep 2s

# Back to the log, then notifications and a profile.
Escape
Sleep 1s
Type "4"
Sleep 1.5s
Type "u"
Sleep 2s
Escape
Sleep 500ms
Type "?"
Sleep 2s
Escape
Sleep 500ms
Type "Q"
Sleep 500ms

Hide
Type "pkill -f ./demo-server; rm -f dn demo-server"
Enter
