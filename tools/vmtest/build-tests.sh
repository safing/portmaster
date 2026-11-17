#!/usr/bin/env bash
# Compile the Go tests of all packages for a target OS into a bundle that can
# run without a Go toolchain:
#
#   build-tests.sh linux|windows OUTDIR
#
# The bundle mirrors the repository layout: every package directory holds its
# compiled test binary (pkg.test or pkg.test.exe) and all non-Go files of the
# package (testdata, fixtures), as tests run from their package directory.
# Linux tests are built with the race detector.
source "$(dirname "${BASH_SOURCE[0]}")/common.sh"

target="${1:-}"
out="${2:-}"
[[ "$target" == linux || "$target" == windows ]] && [[ -n "$out" ]] || die "usage: build-tests.sh linux|windows OUTDIR"

GO="$(go_bin)"
export GOOS="$target" GOARCH=amd64
build_flags=()
ext=""
if [[ "$target" == linux ]]; then
	export CGO_ENABLED=1
	build_flags=(-race)
else
	export CGO_ENABLED=0
	ext=".exe"
fi

cd "$VMTEST_REPO"
rm -rf "$out" && mkdir -p "$out"

# Non-Go files of the repository, without sources of other languages and
# build output, so that tests find their testdata.
log "copying test data"
rsync -a --prune-empty-dirs \
	--exclude '.git/' --exclude 'node_modules/' --exclude 'desktop/' \
	--exclude 'target/' --exclude 'dist/' \
	--exclude '*.go' --exclude 'tools/vmtest/' \
	--include '*/' --include '**/testdata/**' --include '*' \
	./ "$out/"

# Packages with tests for the target OS.
mapfile -t pkgs < <("$GO" list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}|{{.Dir}}{{end}}' ./... | sed '/^$/d')
log "compiling ${#pkgs[@]} test packages for $target"

build_one() {
	local entry="$1" pkg dir rel
	pkg="${entry%%|*}"
	dir="${entry#*|}"
	rel="${dir#"$VMTEST_REPO"/}"
	mkdir -p "$out/$rel"
	if ! "$GO" test -c "${build_flags[@]}" -o "$out/$rel/pkg.test$ext" "$pkg" >"$out/$rel/build.log" 2>&1; then
		echo "BUILD FAILED: $pkg" >&2
		cat "$out/$rel/build.log" >&2
		return 1
	fi
	rm -f "$out/$rel/build.log"
}
export -f build_one log die
export GO out ext VMTEST_REPO
export build_flags_str="${build_flags[*]}"

failed=0
printf '%s\n' "${pkgs[@]}" | xargs -P "$(nproc)" -I{} bash -c 'build_flags=($build_flags_str); build_one "$@"' _ {} || failed=1

# Runner scripts.
cp "$VMTEST_REPO/tools/vmtest/run-tests.sh" "$VMTEST_REPO/tools/vmtest/run-tests.ps1" "$out/"

count=$(find "$out" -name "pkg.test$ext" | wc -l)
log "bundle in $out: $count test binaries, $(du -sh "$out" | cut -f1)"
((failed == 0)) || die "some packages failed to compile"
