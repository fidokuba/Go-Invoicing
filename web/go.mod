// This file exists solely to make /web its own, separate Go module
// boundary — deliberately empty of any real dependency. It has nothing
// to do with the frontend itself (which is plain TypeScript/npm; see
// package.json).
//
// Without it, `go build ./...` / `go vet ./...` / `go test ./...` run
// from the repository root treat every .go file anywhere under this
// directory as part of the go-invoicing module's own tree — including
// any stray .go file an npm dependency happens to ship (this is real,
// not hypothetical: web/node_modules/flatted ships a Go reference
// implementation under its own "golang/" subdirectory). A nested go.mod
// makes Go's tooling treat everything below this point as a separate
// module, which `./...` from the repository root automatically excludes
// — exactly the fence this directory needs, with no effect on the
// frontend build/test/lint tooling, which never invokes `go` at all.
module go-invoicing/web-placeholder

go 1.26.0
