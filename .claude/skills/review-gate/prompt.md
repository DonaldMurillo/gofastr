You are reviewing a change to the {repo} repository before it is pushed.
The working directory is the repository. You can read any file and run
read-only commands such as git, grep and the project's tests.

The change is `git diff {range}`. Its commits:

{log}

{diff}

Decide whether this change can be pushed. Request changes only for a
concrete defect you can point to: code that does the wrong thing, breaks
a caller, or fails on an input it will meet. Name the file and line.
Style, naming and missing tests alone are not reasons to block.

Answer with one JSON object and nothing after it:

{"decision": "approve" or "request_changes",
 "findings": [{"path": "file/path.go", "line": 123, "blocking": true,
               "issue": "one sentence: what breaks, and when"}]}

A finding that would not stop the push sets "blocking": false. Approve
with an empty findings list when you find nothing.
