# Sourced by .githooks/pre-commit and .githooks/pre-push.
#
# Git runs hooks with its own libexec/git-core at the front of PATH. Tests
# that run go or git record PATH in their Go test cache key, so with that
# entry left in, a hook's sweep and a `make test` from the shell never
# share a cached result. Drop it, unless git itself is only reachable
# through it (a client that runs git by absolute path).
stripped=${PATH#"$(git --exec-path):"}
if (PATH=$stripped; command -v git) >/dev/null 2>&1; then
  PATH=$stripped
fi
unset stripped
