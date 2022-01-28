	"github.com/cockroachdb/errors"
	}{{
		input: []byte(
			"2061ba96d63cba38f20a76f039cf29ef68736b8a\x00\x00HEAD\x00Camden Cheek\x00camden@sourcegraph.com\x001632251505\x00Camden Cheek\x00camden@sourcegraph.com\x001632251505\x00fix import\n\x005230097b75dcbb2c214618dd171da4053aff18a6\x00\x00" +
				"5230097b75dcbb2c214618dd171da4053aff18a6\x00\x00HEAD\x00Camden Cheek\x00camden@sourcegraph.com\x001632248499\x00Camden Cheek\x00camden@sourcegraph.com\x001632248499\x00only set matches if they exist\n\x00\x00",
		),
		expected: []*RawCommit{{
			Hash:           []byte("2061ba96d63cba38f20a76f039cf29ef68736b8a"),
			RefNames:       []byte(""),
			SourceRefs:     []byte("HEAD"),
			AuthorName:     []byte("Camden Cheek"),
			AuthorEmail:    []byte("camden@sourcegraph.com"),
			AuthorDate:     []byte("1632251505"),
			CommitterName:  []byte("Camden Cheek"),
			CommitterEmail: []byte("camden@sourcegraph.com"),
			CommitterDate:  []byte("1632251505"),
			Message:        []byte("fix import"),
			ParentHashes:   []byte("5230097b75dcbb2c214618dd171da4053aff18a6"),
		}, {
			Hash:           []byte("5230097b75dcbb2c214618dd171da4053aff18a6"),
			RefNames:       []byte(""),
			SourceRefs:     []byte("HEAD"),
			AuthorName:     []byte("Camden Cheek"),
			AuthorEmail:    []byte("camden@sourcegraph.com"),
			AuthorDate:     []byte("1632248499"),
			CommitterName:  []byte("Camden Cheek"),
			CommitterEmail: []byte("camden@sourcegraph.com"),
			CommitterDate:  []byte("1632248499"),
			Message:        []byte("only set matches if they exist"),
			ParentHashes:   []byte(""),
		}},
	}}