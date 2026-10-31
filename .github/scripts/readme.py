#!/usr/bin/env python3
"""Build README Go snippets and run their echo/timeout examples natively."""
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import tempfile
import time

repo = Path(__file__).resolve().parents[2]
blocks = re.findall(r"```go\n(.*?)\n```", (repo / "README.md").read_text(), re.S)
assert len(blocks) == 3, "review README extraction after adding/removing snippets"
with tempfile.TemporaryDirectory(prefix="sctp-readme-") as directory:
    root = Path(directory)
    (root / "go.mod").write_text('module example.com/sctp-readme\n\ngo 1.26.0\nrequire github.com/skill215/sctp v0.0.0\nreplace github.com/skill215/sctp => ' + json.dumps(str(repo)) + '\n')
    reserve = socket.socket(socket.AF_INET, socket.SOCK_STREAM, 132)
    reserve.bind(("127.0.0.1", 0))
    port = reserve.getsockname()[1]
    reserve.close()
    timeout_main = '''
func main() {
 addr,err:=sctp.ResolveSCTPAddr("sctp4","127.0.0.1:PORT");if err!=nil{panic(err)}
 c,err:=dialWithTimeout(addr);if err!=nil{panic(err)};defer c.Close()
 if err:=c.SubscribeEvents(sctp.SCTP_EVENT_DATA_IO);err!=nil{panic(err)}
 if _,err:=c.SCTPWrite([]byte("timeout example"),&sctp.SndRcvInfo{Stream:1,PPID:73});err!=nil{panic(err)}
 b:=make([]byte,128);n,info,err:=c.SCTPRead(b)
 if err!=nil{panic(err)}
 if string(b[:n])!="timeout example"||info==nil||info.Stream!=1||info.PPID!=73{panic("timeout echo mismatch")}
}
'''.replace("PORT", str(port))
    for name, source in zip(["server", "client", "timeout"], blocks):
        path = root / name
        path.mkdir()
        if name == "timeout":
            source = "package main\n" + source + timeout_main
        source = source.replace("127.0.0.1:9000", "127.0.0.1:" + str(port))
        (path / "main.go").write_text(source)
        subprocess.run(["go", "build", "-buildvcs=false", "-o", str(root / (name + ".bin")), "./" + name], cwd=root, check=True, timeout=180)
    with (root / "server.log").open("w+") as log:
        server = subprocess.Popen([str(root / "server.bin")], stdout=log, stderr=log)
        try:
            for _ in range(100):
                log.seek(0)
                if "listening on" in log.read():
                    break
                if server.poll() is not None:
                    raise RuntimeError("README server exited before listening")
                time.sleep(0.05)
            else:
                raise RuntimeError("README server failed to become ready")
            client = subprocess.run([str(root / "client.bin")], capture_output=True, text=True, check=True, timeout=15)
            assert 'payload="hello SCTP"' in client.stdout and "Stream:0" in client.stdout and "PPID:42" in client.stdout, client.stdout
            subprocess.run([str(root / "timeout.bin")], check=True, timeout=15)
            print("README echo and timeout: payload/stream/PPID verified")
        finally:
            server.terminate()
            server.wait(timeout=10)
