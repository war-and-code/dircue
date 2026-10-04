"""Capture smoke-command output and clean up its process group on timeout."""

import locale
import os
import signal
import subprocess


def run(command, *, timeout, capture_output=False, check=False, **options):
    """A subprocess.run subset for noninteractive verification commands.

    Preserve pipe EOF semantics when a child inherits captured output. POSIX
    commands have a separate session; Windows uses taskkill's process-tree
    cleanup while the parent is alive. Cleanup communication is also timed.
    This is not containment for a process that deliberately detaches itself.
    """
    if capture_output:
        if "stdout" in options or "stderr" in options:
            raise ValueError("capture_output cannot be combined with stdout or stderr")
        options.update(stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    text = options.pop("text", False)
    encoding = options.pop("encoding", None)
    errors = options.pop("errors", None)
    decode = text or encoding is not None or errors is not None
    # Read bytes on every OS. Windows text-reader threads can otherwise lose
    # an incomplete UTF-8 prefix while cleaning up a timed-out process.

    def decoded(value):
        if not decode or value is None:
            return value
        return value.decode(encoding or locale.getpreferredencoding(False), errors or "strict").replace("\r\n", "\n").replace("\r", "\n")

    if os.name == "nt":
        options["creationflags"] = options.get("creationflags", 0) | subprocess.CREATE_NEW_PROCESS_GROUP
    else:
        options["start_new_session"] = True

    process = subprocess.Popen(command, **options)
    try:
        stdout, stderr = process.communicate(timeout=timeout)
    except (subprocess.TimeoutExpired, KeyboardInterrupt) as error:
        try:
            if os.name == "nt":
                subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                               timeout=5, check=False)
            else:
                os.killpg(process.pid, signal.SIGKILL)
        except (OSError, subprocess.TimeoutExpired):
            # Still terminate the direct child if tree cleanup raced with exit
            # or the host's cleanup utility failed.
            pass
        if process.poll() is None:
            try:
                process.kill()
            except OSError:
                pass
        try:
            stdout, stderr = process.communicate(timeout=5)
        except (subprocess.TimeoutExpired, UnicodeError):
            # A detached child or an already-dead Windows parent can prevent
            # tree cleanup. Keep the original timeout and partial diagnostics;
            # never wait without a bound for that child's inherited pipes or
            # replace the timeout with decoding of an incomplete UTF-8 prefix.
            pass
        else:
            if isinstance(error, subprocess.TimeoutExpired):
                if error.output is None:
                    error.output = stdout
                if error.stderr is None:
                    error.stderr = stderr
        process.wait(timeout=5)
        raise
    result = subprocess.CompletedProcess(command, process.returncode, decoded(stdout), decoded(stderr))
    if check and result.returncode:
        raise subprocess.CalledProcessError(result.returncode, command, result.stdout, result.stderr)
    return result
