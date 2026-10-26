# /// script
# requires-python = ">=3.11"
# dependencies = ["pyte==0.8.2"]
# ///
"""Real PTY interaction checks using --demo and isolated preferences only."""

import codecs
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time

import pyte


class Terminal:
    def __init__(self, executable, directory, columns=140, rows=36):
        self.master, self.slave = pty.openpty()
        self.before = termios.tcgetattr(self.slave)
        self.screen = pyte.Screen(columns, rows)
        self.stream = pyte.Stream(self.screen)
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.raw = bytearray()
        self.resize(columns, rows, notify=False)
        environment = dict(
            os.environ,
            TERM="xterm-256color",
            XDG_CONFIG_HOME=str(directory / "config"),
            XDG_CACHE_HOME=str(directory / "cache"),
            NO_COLOR="1",
        )
        environment.pop("BW_SESSION", None)
        self.process = subprocess.Popen(
            [str(executable), "--demo", "--source", "bitwarden"],
            stdin=self.slave,
            stdout=self.slave,
            stderr=self.slave,
            env=environment,
        )

    def resize(self, columns, rows, notify=True):
        fcntl.ioctl(
            self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, columns, 0, 0)
        )
        self.screen.resize(rows, columns)
        if notify:
            self.process.send_signal(signal.SIGWINCH)

    def pump(self, timeout=0.1):
        if select.select([self.master], [], [], timeout)[0]:
            try:
                data = os.read(self.master, 65536)
            except OSError:
                return
            self.raw.extend(data)
            self.stream.feed(self.decoder.decode(data))
            # Answer terminal queries rather than waiting on detection timeouts.
            if b"\x1b[6n" in data:
                os.write(self.master, b"\x1b[1;1R")
            if b"\x1b[c" in data:
                os.write(self.master, b"\x1b[?1;2c")

    def text(self):
        return "\n".join(self.screen.display)

    def expect(self, text, timeout=8):
        deadline = time.monotonic() + timeout
        while text not in self.text():
            if self.process.poll() is not None:
                raise AssertionError(f"TUI exited while waiting for {text!r}")
            if time.monotonic() >= deadline:
                raise AssertionError(f"Missing {text!r}\n{self.text()}")
            self.pump()

    def send(self, value):
        os.write(self.master, value)
        # A single read can return before the escape-key ambiguity timeout.
        # Keep separate key presses separate from Alt-prefixed sequences.
        deadline = time.monotonic() + 0.25
        while time.monotonic() < deadline:
            self.pump(0.05)

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)
        after = termios.tcgetattr(self.slave)
        # macOS sets PENDIN when canonical input is restored. It is transient
        # pending-input state, not a changed echo/raw/processing preference.
        after[3] &= ~getattr(termios, "PENDIN", 0)
        before = list(self.before)
        before[3] &= ~getattr(termios, "PENDIN", 0)
        restored = after == before
        if not restored:
            print(f"termios before={self.before!r} after={after!r}", file=sys.stderr)
        os.close(self.master)
        os.close(self.slave)
        return restored


def main():
    executable = Path(sys.argv[1]).resolve()
    with tempfile.TemporaryDirectory(prefix="lazycred-pty-") as directory:
        terminal = Terminal(executable, Path(directory))
        checks = []
        try:
            terminal.expect("Example Cloud")
            terminal.expect("Password")
            if "demo-only-value" in terminal.text():
                raise AssertionError("secret visible before reveal")
            checks.append("startup and masked details")

            terminal.send(b"/")
            terminal.send(b"jkhql/?")
            terminal.expect("No matching items")
            terminal.send(b"\x1b")
            terminal.expect("Example Cloud")
            checks.append("filter input owns printable shortcuts")

            terminal.send(b"u")
            terminal.expect("Master password")
            terminal.send(b"\x1b[200~PUBLIC_PASTE_TEST\nq\x1b[201~")
            if b"PUBLIC_PASTE_TEST" in terminal.raw:
                raise AssertionError("masked password input echoed")
            terminal.send(b"\x1b")
            checks.append("masked form, bracketed paste, cancellation")

            terminal.send(b"?")
            terminal.expect("Current actions")
            terminal.send(b"\x1b")
            terminal.send(b"v")
            terminal.expect("Reveal field")
            terminal.send(b"\r")
            terminal.expect("demo-only-value")
            terminal.send(b"L")
            terminal.expect("Demo vault is locked.")
            if "demo-only-value" in terminal.text():
                raise AssertionError("lock retained a visible secret")
            checks.append("help, explicit reveal and local lock")

            terminal.resize(80, 24)
            terminal.expect("lazycred")
            terminal.send(b"s")
            terminal.expect("Sources")
            terminal.send(b"\x1b")
            terminal.resize(45, 14)
            terminal.expect("lazycred")
            terminal.resize(140, 36)
            terminal.expect("lazycred")
            checks.append("wide, 80x24 and narrow resize")

            terminal.send(b"q")
            terminal.process.wait(timeout=5)
            if terminal.process.returncode != 0:
                raise AssertionError("normal quit failed")
            checks.append("normal exit")
        finally:
            restored = terminal.close()
        if not restored:
            raise AssertionError("terminal modes were not restored")
        checks.append("terminal echo and input modes restored")
        print(
            json.dumps(
                {"pty_checks": checks, "real_resources_accessed": False}, indent=2
            )
        )


if __name__ == "__main__":
    main()
