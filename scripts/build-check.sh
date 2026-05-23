#!/bin/bash
# Compile check for the worktree
cd "$(dirname "$0")/.." 
go build ./framework/routegroup/ 2>&1
echo "RC=$?"
