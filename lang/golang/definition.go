package golang

import (
	"errors"
	"io/ioutil"

	"sourcegraph.com/sourcegraph/sourcegraph/pkg/jsonrpc2"
	"sourcegraph.com/sourcegraph/sourcegraph/pkg/lsp"
)

func (h *Session) handleDefinition(req *jsonrpc2.Request, params lsp.TextDocumentPositionParams) ([]lsp.Location, error) {
	// Find start of definition using guru
	contents, err := h.readFile(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	ofs, valid := offsetForPosition(contents, params.Position)
	if !valid {
		return nil, errors.New("invalid position")
	}
	def, err := godef(h.filePath("gopath"), h.filePath(params.TextDocument.URI), int(ofs))

	uri, err := h.fileURI(def.Path)
	if err != nil {
		return nil, err
	}
	if uri != params.TextDocument.URI {
		// different file to input
		contents, err = ioutil.ReadFile(def.Path)
		if err != nil {
			return nil, err
		}
	}
	r, err := rangeAtPosition(lsp.Position{Line: def.Line - 1, Character: def.Column - 1}, contents)
	if err != nil {
		return nil, err
	}

	var locs []lsp.Location
	locs = append(locs, lsp.Location{
		URI:   uri,
		Range: r,
	})
	return locs, nil
}
