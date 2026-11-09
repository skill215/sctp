#!/usr/bin/env python3
"""Compile an independent consumer against this checkout on six targets."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[2]
source = '''package main
import (
 "context"
 "errors"
 "syscall"
 "github.com/skill215/sctp"
)
var _ syscall.Conn = (*sctp.SCTPConn)(nil)
var _ syscall.Conn = (*sctp.SCTPListener)(nil)
func main() {
 _ = errors.Is(nil, sctp.ErrUnsupported)
 m := sctp.MessageOptions{ReceiveInfo:sctp.ReceiveInfoSNDRCV, AllowFragmentation:true}
 _, _ = sctp.MessageIORequirements(m)
 _, _ = sctp.ParseNotification(nil)
 var _ func(context.Context, []byte, []byte, []byte) (sctp.ReadResult,error) = (*sctp.SCTPConn)(nil).ReadEvent
 var _ func(context.Context, []byte, sctp.SndInfo) (int,error) = (*sctp.SCTPConn)(nil).WriteMsgContext
 var _ func() (sctp.MessageOptionState,error) = (*sctp.SCTPConn)(nil).GetMessageOptions
 var _ func(context.Context,string,*sctp.SCTPAddr,*sctp.SCTPAddr)(*sctp.SCTPConn,error) = (&sctp.SocketConfig{Messages:&m}).DialContext
 _ = sctp.Notification{Kind:sctp.NotificationPartialDelivery}
}
'''
with tempfile.TemporaryDirectory(prefix="sctp-consumer-") as directory:
    root = Path(directory)
    (root / "main.go").write_text(source)
    (root / "go.mod").write_text('module example.com/sctp-consumer\n\ngo 1.26.0\nrequire github.com/skill215/sctp v0.0.0\nreplace github.com/skill215/sctp => ' + json.dumps(str(repo)) + '\n')
    for target, arch in [("linux", "amd64"), ("linux", "arm64"), ("linux", "386"), ("darwin", "amd64"), ("freebsd", "amd64"), ("windows", "amd64")]:
        env = dict(os.environ, GOOS=target, GOARCH=arch, CGO_ENABLED="0")
        subprocess.run(["go", "build", "-buildvcs=false", "-o", str(root / "consumer"), "."], cwd=root, env=env, check=True, timeout=180)
        print(json.dumps({"target": target + "/" + arch, "consumer": "pass"}), flush=True)
