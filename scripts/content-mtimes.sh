#!/bin/bash
# Give every tracked file an mtime derived from its content, so Go's test
# cache can replay a CI run from the previous one.
#
# Go keys a cached test result on the files the test opened, hashed by
# size and mtime, not content. actions/checkout writes every file fresh,
# so in CI every test that reads a repo file (testdata, golden files,
# docs) missed the cache on every run: 0 of 158 packages replayed on a
# push that changed one test file. Here a file's mtime comes from its git
# blob hash, so unchanged content keeps the same mtime from run to run
# and changed content gets a new one. Directories, whose listings Go also
# hashes, all get one fixed mtime, untracked ones included (a directory an
# earlier CI step created is in its parent's listing too). Go hashes a
# listing's entry names as well, so a file added or removed still changes
# it. .git and HEAD get the fixed time too: a root listing carries .git's
# mtime, and framework/isolation stats .git/HEAD. HEAD holds the merge
# commit's SHA, new on every run, and no test reads it, so its time stays
# fixed rather than following its content. The index is refreshed first so
# the git calls the test step makes later find nothing to rewrite.
#
# The times sit in 2001-2009, so Go's "modified in the last two seconds"
# refusal never applies. Symlinks and submodules are left alone.
#
# Run after checkout and before any test. It refuses a tree with
# uncommitted changes: the hash it reads is the index entry's, and a file
# edited since would keep its old mtime with new content, which Go would
# then replay a stale result for.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
	echo "content-mtimes: the tree has uncommitted changes; run it on a clean checkout" >&2
	git status --short --untracked-files=no >&2
	exit 1
fi

git ls-files -s -z | perl -0 -ne '
	chomp;
	my ($meta, $path) = split /\t/, $_, 2;
	my (undef, $sha) = split / /, $meta;
	next if -l $path || ! -f $path;
	my $t = 978307200 + hex(substr($sha, 0, 7));
	utime($t, $t, $path) or die "utime $path: $!\n";
'

perl -MFile::Find -e '
	find({ no_chdir => 1, wanted => sub {
		if ($_ eq "./.git") { $File::Find::prune = 1; return }
		utime(978307200, 978307200, $_) or die "utime $_: $!\n" if -d $_ && ! -l $_;
	} }, ".");
'

git update-index -q --refresh
perl -e 'for (@ARGV) { utime(978307200, 978307200, $_) or die "utime $_: $!\n" }' \
	"$(git rev-parse --git-path HEAD)" .git
