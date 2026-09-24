package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	hereActive    = "aaaa1111-1111-4111-8111-111111111111"
	otherActive   = "aaaa2222-2222-4222-8222-222222222222"
	subActive     = "aaaa3333-3333-4333-8333-333333333333"
	hereArchived  = "bbbb1111-1111-4111-8111-111111111111"
	otherArchived = "bbbb2222-2222-4222-8222-222222222222"
	hereTrashed   = "cccc1111-1111-4111-8111-111111111111"
	otherTrashed  = "cccc2222-2222-4222-8222-222222222222"
)

type cliFixture struct {
	st          store
	here, other string
}

// cliStore is a store behind the environment configDir and dataDir read, with
// the working directory set to the cwd of the "here" sessions. The other
// sessions sit in another project, so resolving them shows an id reaches past
// the scope ls uses. PATH is emptied so that a resume the test did not mean to
// reach cannot exec the real claude over the test binary.
func cliStore(t *testing.T) cliFixture {
	t.Helper()
	xdg := t.TempDir()
	f := cliFixture{st: store{cfg: t.TempDir(), data: filepath.Join(xdg, "ccarchive")}, other: t.TempDir()}
	t.Setenv("CLAUDE_CONFIG_DIR", f.st.cfg)
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	f.here = here
	put := func(root, proj, uuid, cwd, title string) {
		write(t, filepath.Join(root, proj, uuid+".jsonl"),
			`{"type":"custom-title","customTitle":"`+title+`"}`+"\n"+`{"type":"user","cwd":"`+cwd+`"}`+"\n", time.Now())
	}
	active, archive, trash := activeRoot(f.st.cfg), archiveRoot(f.st.data), trashRoot(f.st.data)
	put(active, project, hereActive, here, "here active")
	put(active, "-other", otherActive, f.other, "other active")
	put(active, project+"-sub", subActive, filepath.Join(here, "sub"), "sub active")
	put(archive, project, hereArchived, here, "here archived")
	put(archive, "-other", otherArchived, f.other, "other archived")
	put(trash, project, hereTrashed, here, "here trashed")
	put(trash, "-other", otherTrashed, f.other, "other trashed")
	sc := sidecarJSON(t, time.Now(), originActive)
	write(t, filepath.Join(trash, project, hereTrashed+".trashed"), sc, time.Now())
	write(t, filepath.Join(trash, "-other", otherTrashed+".trashed"), sc, time.Now())
	return f
}

func execute(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := rootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestLsPrintsTabSeparatedUUIDStateCwdTitleForEachFlag(t *testing.T) {
	f := cliStore(t)
	line := func(uuid, state, cwd, title string) string {
		return strings.Join([]string{uuid, state, cwd, title}, "\t")
	}
	var (
		ha = line(hereActive, "active", f.here, "here active")
		oa = line(otherActive, "active", f.other, "other active")
		sa = line(subActive, "active", filepath.Join(f.here, "sub"), "sub active")
		hA = line(hereArchived, "archived", f.here, "here archived")
		oA = line(otherArchived, "archived", f.other, "other archived")
		ht = line(hereTrashed, "trashed", f.here, "here trashed")
		ot = line(otherTrashed, "trashed", f.other, "other trashed")
	)
	for _, tc := range []struct {
		flags []string
		want  []string
	}{
		{nil, []string{ha}},
		{[]string{"--all"}, []string{ha, oa, sa}},
		{[]string{"--archived"}, []string{ha, hA}},
		{[]string{"--all", "--archived"}, []string{ha, oa, sa, hA, oA}},
		{[]string{"--trash"}, []string{ht}},
		{[]string{"--trash", "--all"}, []string{ht, ot}},
	} {
		t.Run(strings.Join(tc.flags, " "), func(t *testing.T) {
			out, _, err := execute(t, append([]string{"ls"}, tc.flags...)...)
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			slices.Sort(got)
			slices.Sort(tc.want)
			if !slices.Equal(got, tc.want) {
				t.Errorf("ls %v =\n%s\nwant\n%s", tc.flags, strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestLsEscapesTabNewlineAndBackslashSoEachSessionIsOneFourFieldLine(t *testing.T) {
	f := cliStore(t)
	write(t, filepath.Join(activeRoot(f.st.cfg), project, hereActive+".jsonl"),
		`{"type":"custom-title","customTitle":"a\tb\nc\r\\d"}`+"\n"+`{"type":"user","cwd":"`+f.here+`"}`+"\n", time.Now())
	out, _, err := execute(t, "ls")
	if err != nil {
		t.Fatal(err)
	}
	want := hereActive + "\tactive\t" + f.here + "\t" + `a\tb\nc\r\\d` + "\n"
	if out != want {
		t.Errorf("ls = %q, want %q", out, want)
	}
}

func TestIdResolvesFullUUIDOrUniquePrefixInAnyStoreAndProject(t *testing.T) {
	f := cliStore(t)
	for _, tc := range []struct {
		args     []string
		from, to string
	}{
		{[]string{"archive", otherActive}, filepath.Join(activeRoot(f.st.cfg), "-other"), filepath.Join(archiveRoot(f.st.data), "-other")},
		{[]string{"unarchive", "bbbb2"}, filepath.Join(archiveRoot(f.st.data), "-other"), filepath.Join(activeRoot(f.st.cfg), "-other")},
		{[]string{"restore", "cccc2"}, filepath.Join(trashRoot(f.st.data), "-other"), filepath.Join(activeRoot(f.st.cfg), "-other")},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			if _, stderr, err := execute(t, tc.args...); err != nil {
				t.Fatalf("%v: %s", err, stderr)
			}
			sessions, err := listSessions(f.st.cfg, f.st.data)
			if err != nil {
				t.Fatal(err)
			}
			s, err := resolve(sessions, tc.args[1])
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Dir(s.Path) != tc.to {
				t.Errorf("%s is in %s, want %s", s.UUID, filepath.Dir(s.Path), tc.to)
			}
			mustNotExist(t, filepath.Join(tc.from, s.UUID+".jsonl"))
		})
	}
}

func TestAmbiguousOrUnmatchedIdFailsNamingMatchesAndMovesNothing(t *testing.T) {
	for _, tc := range []struct {
		id    string
		names []string
	}{
		{"aaaa", []string{hereActive, otherActive, subActive}},
		{"ffff", nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			f := cliStore(t)
			before, err := listSessions(f.st.cfg, f.st.data)
			if err != nil {
				t.Fatal(err)
			}
			for _, cmd := range []string{"archive", "unarchive", "trash", "restore", "purge", "resume"} {
				_, stderr, err := execute(t, cmd, tc.id)
				if err == nil {
					t.Errorf("%s %s succeeded", cmd, tc.id)
				}
				for _, u := range tc.names {
					if !strings.Contains(stderr, u) {
						t.Errorf("%s %s: stderr %q does not name %s", cmd, tc.id, stderr, u)
					}
				}
			}
			after, err := listSessions(f.st.cfg, f.st.data)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(before, after) {
				t.Errorf("sessions changed:\n%v\n%v", before, after)
			}
		})
	}
}

// The move functions would mostly fail on such a session anyway, so the test
// asserts the refusal the guard gives rather than only a failure.
func TestRestoreAndPurgeRefuseOutsideTrashAndOtherCommandsInIt(t *testing.T) {
	f := cliStore(t)
	before, err := listSessions(f.st.cfg, f.st.data)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"restore", hereActive}, "is not in the trash"},
		{[]string{"purge", hereArchived}, "is not in the trash"},
		{[]string{"archive", hereTrashed}, "is in the trash"},
		{[]string{"unarchive", hereTrashed}, "is in the trash"},
		{[]string{"trash", hereTrashed}, "is in the trash"},
		{[]string{"resume", hereTrashed}, "is in the trash"},
	} {
		if _, stderr, err := execute(t, tc.args...); err == nil || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: err %v, stderr %q, want it to say %q", tc.args, err, stderr, tc.want)
		}
	}
	after, err := listSessions(f.st.cfg, f.st.data)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(before, after) {
		t.Errorf("sessions changed:\n%v\n%v", before, after)
	}
}

func TestSessionCommandsRefuseAnyArgumentCountButOne(t *testing.T) {
	cliStore(t)
	for _, args := range [][]string{{"archive"}, {"purge", hereTrashed, hereArchived}, {"ls", "x"}} {
		if _, _, err := execute(t, args...); err == nil {
			t.Errorf("%v succeeded", args)
		}
	}
}
