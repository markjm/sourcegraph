package perforce

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/sourcegraph/sourcegraph/internal/authz"
	"github.com/sourcegraph/sourcegraph/internal/extsvc"
)

func TestConvertToPostgresMatch(t *testing.T) {
	// Only needs to implement directory-level perforce protects
	tests := []struct {
		name  string
		match string
		want  string
	}{{
		name:  "*",
		match: "//Sourcegraph/Engineering/*/Frontend/",
		want:  "//Sourcegraph/Engineering/[^/]+/Frontend/",
	}, {
		name:  "...",
		match: "//Sourcegraph/Engineering/.../Frontend/",
		want:  "//Sourcegraph/Engineering/%/Frontend/",
	}, {
		name:  "* and ...",
		match: "//Sourcegraph/*/Src/.../Frontend/",
		want:  "//Sourcegraph/[^/]+/Src/%/Frontend/",
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertToPostgresMatch(tt.match)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestConvertToGlobMatch(t *testing.T) {
	// Should fully implement perforce protects
	// Some cases taken directly from https://www.perforce.com/manuals/cmdref/Content/CmdRef/filespecs.html
	// Useful for debugging:
	//
	//   go run github.com/gobwas/glob/cmd/globdraw -p '{//gra*/dep*/,//gra*/dep*}' -s '/' | dot -Tpng -o pattern.png
	//
	tests := []struct {
		name  string
		match string
		want  string

		shouldMatch    []string
		shouldNotMatch []string
	}{{
		name:  "*",
		match: "//Sourcegraph/Engineering/*/Frontend/",
		want:  "//Sourcegraph/Engineering/*/Frontend/",
	}, {
		name:  "...",
		match: "//Sourcegraph/Engineering/.../Frontend/",
		want:  "//Sourcegraph/Engineering/**/Frontend/",
	}, {
		name:           "* and ...",
		match:          "//Sourcegraph/*/Src/.../Frontend/",
		want:           "//Sourcegraph/*/Src/**/Frontend/",
		shouldMatch:    []string{"//Sourcegraph/Path/Src/One/Two/Frontend/"},
		shouldNotMatch: []string{"//Sourcegraph/One/Two/Src/Path/Frontend/"},
	}, {
		name:  "./....c",
		match: "./....c",
		want:  "./**.c",
		shouldMatch: []string{
			"./file.c", "./dir/file.c",
		},
	}, {
		name:  "//gra*/dep*",
		match: "//gra*/dep*",
		want:  `//gra*/dep*{/,}`,
		shouldMatch: []string{
			"//graph/depot/", "//graphs/depots",
		},
		shouldNotMatch: []string{"//graph/depot/release1/"},
	}, {
		name:        "//depot/main/rel...",
		match:       "//depot/main/rel...",
		want:        "//depot/main/rel**",
		shouldMatch: []string{"//depot/main/rel/", "//depot/main/releases/", "//depot/main/release-note.txt", "//depot/main/rel1/product1"},
	}, {
		name:        "//app/*",
		match:       "//app/*",
		want:        "//app/*{/,}",
		shouldMatch: []string{"//app/main", "//app/main/"},
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertToGlobMatch(tt.match)
			if err != nil {
				t.Fatal(fmt.Sprintf("unexpected error: %+v", err))
			}
			if diff := cmp.Diff(tt.want, got.pattern); diff != "" {
				t.Fatal(diff)
			}
			if len(tt.shouldMatch) > 0 {
				for _, m := range tt.shouldMatch {
					if !got.Match(m) {
						t.Errorf("%q should have matched %q", got.pattern, m)
					}
				}
			}
			if len(tt.shouldNotMatch) > 0 {
				for _, m := range tt.shouldNotMatch {
					if got.Match(m) {
						t.Errorf("%q should not have matched %q", got.pattern, m)
					}
				}
			}
		})
	}
}

func mustGlob(t *testing.T, match string) globMatch {
	m, err := convertToGlobMatch(match)
	if err != nil {
		t.Error(err)
	}
	return m
}

// mustGlobPattern gets the glob pattern for a given p4 match for use in testing
func mustGlobPattern(t *testing.T, match string) string {
	return mustGlob(t, match).pattern
}

func TestMatchesAgainstDepot(t *testing.T) {
	type args struct {
		match globMatch
		depot string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{{
		name: "simple match",
		args: args{
			match: mustGlob(t, "//app/main/..."),
			depot: "//app/main/",
		},
		want: true,
	}, {
		name: "no wildcard in match",
		args: args{
			match: mustGlob(t, "//app/"),
			depot: "//app/main/",
		},
		want: false,
	}, {
		name: "match parent path",
		args: args{
			match: mustGlob(t, "//app/..."),
			depot: "//app/main/",
		},
		want: true,
	}, {
		name: "match sub path with all wildcard",
		args: args{
			match: mustGlob(t, "//app/.../file"),
			depot: "//app/main/",
		},
		want: true,
	}, {
		name: "match sub path with dir wildcard",
		args: args{
			match: mustGlob(t, "//app/*/file"),
			depot: "//app/main/",
		},
		want: true,
	}, {
		name: "match sub path with dir and all wildcards",
		args: args{
			match: mustGlob(t, "//app/*/file/.../path"),
			depot: "//app/main/",
		},
		want: true,
	}, {
		name: "match sub path with dir wildcard that's deeply nested",
		args: args{
			match: mustGlob(t, "//app/*/file/*/another-file/path/"),
			depot: "//app/main/",
		},
		want: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesAgainstDepot(tt.args.match, tt.args.depot); got != tt.want {
				t.Errorf("matchesAgainstDepot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScanFullRepoPermissions(t *testing.T) {
	f, err := os.Open("testdata/sample-protects-u.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rc := io.NopCloser(bytes.NewReader(data))

	execer := p4ExecFunc(func(ctx context.Context, host, user, password string, args ...string) (io.ReadCloser, http.Header, error) {
		return rc, nil, nil
	})

	p := NewTestProvider("", "ssl:111.222.333.444:1666", "admin", "password", execer)
	p.depots = []extsvc.RepoID{
		"//app/main/",
		"//app/training/",
		"//app/test/",
		"//app/rickroll/",
		"//not-app/not-main/", // no rules exist
	}
	perms := &authz.ExternalUserPermissions{
		SubRepoPermissions: make(map[extsvc.RepoID]*authz.SubRepoPermissions),
	}
	if err := scanProtects(rc, fullRepoPermsScanner(perms, p.depots)); err != nil {
		t.Fatal(err)
	}

	// See sample-protects-u.txt for notes
	want := &authz.ExternalUserPermissions{
		Exacts: []extsvc.RepoID{
			"//app/main/",
			"//app/training/",
			"//app/test/",
		},
		SubRepoPermissions: map[extsvc.RepoID]*authz.SubRepoPermissions{
			"//app/main/": {
				PathIncludes: []string{
					mustGlobPattern(t, "core/..."),
					mustGlobPattern(t, "*/stuff/..."),
					mustGlobPattern(t, "frontend/.../stuff/*"),
					mustGlobPattern(t, "config.yaml"),
					mustGlobPattern(t, "subdir/**"),
					mustGlobPattern(t, ".../README.md"),
					mustGlobPattern(t, "dir.yaml"),
				},
				PathExcludes: []string{
					mustGlobPattern(t, "subdir/remove/"),
					mustGlobPattern(t, "subdir/*/also-remove/..."),
					mustGlobPattern(t, ".../.secrets.env"),
				},
			},
			"//app/test/": {
				PathIncludes: []string{
					mustGlobPattern(t, "..."),
					mustGlobPattern(t, ".../README.md"),
					mustGlobPattern(t, "dir.yaml"),
				},
				PathExcludes: []string{
					mustGlobPattern(t, ".../.secrets.env"),
				},
			},
			"//app/training/": {
				PathIncludes: []string{
					mustGlobPattern(t, "..."),
					mustGlobPattern(t, ".../README.md"),
					mustGlobPattern(t, "dir.yaml"),
				},
				PathExcludes: []string{
					mustGlobPattern(t, "secrets/..."),
					mustGlobPattern(t, ".env"),
					mustGlobPattern(t, ".../.secrets.env"),
				},
			},
		},
	}
	if diff := cmp.Diff(want, perms); diff != "" {
		t.Fatal(diff)
	}
}

func TestScanFullRepoPermissionsWithWildcardMatchingDepot(t *testing.T) {
	f, err := os.Open("testdata/sample-protects-m.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rc := io.NopCloser(bytes.NewReader(data))

	execer := p4ExecFunc(func(ctx context.Context, host, user, password string, args ...string) (io.ReadCloser, http.Header, error) {
		return rc, nil, nil
	})

	p := NewTestProvider("", "ssl:111.222.333.444:1666", "admin", "password", execer)
	p.depots = []extsvc.RepoID{
		"//app/main/core/",
	}
	perms := &authz.ExternalUserPermissions{
		SubRepoPermissions: make(map[extsvc.RepoID]*authz.SubRepoPermissions),
	}
	if err := scanProtects(rc, fullRepoPermsScanner(perms, p.depots)); err != nil {
		t.Fatal(err)
	}

	want := &authz.ExternalUserPermissions{
		Exacts: []extsvc.RepoID{
			"//app/main/core/",
		},
		SubRepoPermissions: map[extsvc.RepoID]*authz.SubRepoPermissions{
			"//app/main/core/": {
				PathIncludes: []string{
					mustGlobPattern(t, "**"),
				},
				PathExcludes: []string{
					mustGlobPattern(t, "**"),
					mustGlobPattern(t, "**/core/build/deleteorgs.txt"),
					mustGlobPattern(t, "build/deleteorgs.txt"),
					mustGlobPattern(t, "**/core/build/**/asdf.txt"),
					mustGlobPattern(t, "build/**/asdf.txt"),
				},
			},
		},
	}

	if diff := cmp.Diff(want, perms); diff != "" {
		t.Fatal(diff)
	}
}

func TestFullScanWildcardDepotMatching(t *testing.T) {
	f, err := os.Open("testdata/sample-protects-x.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rc := io.NopCloser(bytes.NewReader(data))

	execer := p4ExecFunc(func(ctx context.Context, host, user, password string, args ...string) (io.ReadCloser, http.Header, error) {
		return rc, nil, nil
	})

	p := NewTestProvider("", "ssl:111.222.333.444:1666", "admin", "password", execer)
	p.depots = []extsvc.RepoID{
		"//app/236/freeze/core/",
	}
	perms := &authz.ExternalUserPermissions{
		SubRepoPermissions: make(map[extsvc.RepoID]*authz.SubRepoPermissions),
	}
	if err := scanProtects(rc, fullRepoPermsScanner(perms, p.depots)); err != nil {
		t.Fatal(err)
	}

	want := &authz.ExternalUserPermissions{
		Exacts: []extsvc.RepoID{
			"//app/236/freeze/core/",
		},
		SubRepoPermissions: map[extsvc.RepoID]*authz.SubRepoPermissions{
			"//app/236/freeze/core/": {
				PathExcludes: nil,
				PathIncludes: []string{
					mustGlobPattern(t, "db/upgrade-scripts-sfsql/**"),
					mustGlobPattern(t, "db/sayonaradb/upgrade-scripts/**"),
					mustGlobPattern(t, "sfdc/config/sayonara_schema.xml"),
					mustGlobPattern(t, "db/plpgsql/**"),
				},
			},
		},
	}

	if diff := cmp.Diff(want, perms); diff != "" {
		t.Fatal(diff)
	}
}

func TestCheckWildcardDepotMatch(t *testing.T) {
	testDepot := extsvc.RepoID("//app/main/core/")
	testCases := []struct {
		label              string
		pattern            string
		original           string
		expectedNewRules   []string
		expectedFoundMatch bool
		depot              extsvc.RepoID
	}{
		{
			label:            "depot match ends with double wildcard",
			pattern:          "//app/**/README.md",
			original:         "//app/.../README.md",
			expectedNewRules: []string{"**/README.md"},
			depot:            "//app/test/",
		},
		{
			label:            "single wildcard",
			pattern:          "//app/*/dir.yaml",
			original:         "//app/*/dir.yaml",
			expectedNewRules: []string{"dir.yaml"},
			depot:            "//app/test/",
		},
		{
			label:            "single wildcard in depot match",
			pattern:          "//app/**/core/build/deleteorgs.txt",
			original:         "//app/.../core/build/deleteorgs.txt",
			expectedNewRules: []string{"**/core/build/deleteorgs.txt", "build/deleteorgs.txt"},
			depot:            testDepot,
		},
		{
			label:            "ends with wildcard",
			pattern:          "//app/**",
			original:         "//app/...",
			expectedNewRules: []string{"**"},
			depot:            testDepot,
		},
		{
			label:            "two wildcards",
			pattern:          "//app/**/tests/**/my_test",
			original:         "//app/.../test/.../my_test",
			expectedNewRules: []string{"**/tests/**/my_test"},
			depot:            testDepot,
		},
		{
			label:            "no match no effect",
			pattern:          "//foo/**/core/build/asdf.txt",
			original:         "//foo/.../core/build/asdf.txt",
			expectedNewRules: []string{},
			depot:            testDepot,
		},
		{
			label:            "original rule is fine, no changes needed",
			pattern:          "//**/.secrets.env",
			original:         "//.../.secrets.env",
			expectedNewRules: []string{},
			depot:            testDepot,
		},
		{
			label:            "single wildcard match",
			pattern:          "//app/2*/*/core/schema/submodules**",
			original:         "//app/2*/*/core/schema/submodules**",
			expectedNewRules: []string{"schema/submodules**"},
			depot:            "//app/236/freeze/core/",
		},
		{
			label:            "single wildcard match no double wildcard",
			pattern:          "//app/2*/*/core/asdf/java/resources/foo.xml",
			original:         "//app/2*/*/core/asdf/java/resources/foo.xml",
			expectedNewRules: []string{"asdf/java/resources/foo.xml"},
			depot:            "//app/236/freeze/core/",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.label, func(t *testing.T) {
			pattern := tc.pattern
			glob := mustGlob(t, pattern)
			rule := globMatch{
				glob,
				pattern,
				tc.original,
			}
			newRules := convertRulesForWildcardDepotMatch(rule, tc.depot)
			if !reflect.DeepEqual(newRules, tc.expectedNewRules) {
				t.Errorf("expected %v, got %v", tc.expectedNewRules, newRules)
			}
		})
	}
}

func TestScanAllUsers(t *testing.T) {
	ctx := context.Background()
	f, err := os.Open("testdata/sample-protects-a.txt")
	if err != nil {
		t.Fatal(err)
	}

	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rc := io.NopCloser(bytes.NewReader(data))

	execer := p4ExecFunc(func(ctx context.Context, host, user, password string, args ...string) (io.ReadCloser, http.Header, error) {
		return rc, nil, nil
	})

	p := NewTestProvider("", "ssl:111.222.333.444:1666", "admin", "password", execer)
	p.cachedGroupMembers = map[string][]string{
		"dev": {"user1", "user2"},
	}
	p.cachedAllUserEmails = map[string]string{
		"user1": "user1@example.com",
		"user2": "user2@example.com",
	}

	users := make(map[string]struct{})
	if err := scanProtects(rc, allUsersScanner(ctx, p, users)); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{}{
		"user1": {},
		"user2": {},
	}
	if diff := cmp.Diff(want, users); diff != "" {
		t.Fatal(diff)
	}
}
