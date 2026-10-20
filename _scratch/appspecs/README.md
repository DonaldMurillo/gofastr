# App spec gate scratch packages

The app spec gate tests generate an app here, one directory per gate, compile
it, and remove it again (`internal/scratchdir.Package`). The generated code
imports its own packages, so it has to sit inside the repo module.

This file keeps the directory in every checkout. A gate that created it would
change the module root's directory listing in the middle of a test sweep, and
Go's test cache would then re-run every package that walks the repo. Everything
else here is gitignored.
