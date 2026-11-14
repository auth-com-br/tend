package policy

import (
	"strings"
	"testing"
)

var here = Where{Project: "/home/u/code/app", Cwd: "/home/u/code/app/src", Home: "/home/u", Temp: []string{"/tmp"}}

// TestTheGuardCatchesWhatDestroysWorkOutsideTheProject: each line is
// something an agent has been reported to run, or close to it. If one gets
// through, the guard is a promise it does not keep.
func TestTheGuardCatchesWhatDestroysWorkOutsideTheProject(t *testing.T) {
	g := Guard{Mode: GuardDeny}
	for line, rule := range map[string]string{
		"rm -rf /":                                RuleRmOutside,
		"rm -rf ~":                                RuleRmOutside,
		"rm -rf $HOME/":                           RuleRmOutside,
		"sudo rm -rf /var/lib/postgresql":         RuleRmOutside,
		"rm -r ../../other-project":               RuleRmOutside,
		"cd /tmp && rm -fr /home/u/code/app":      RuleRmOutside,
		"rm -rf ~/Documents":                      RuleRmOutside,
		`bash -c "rm -rf /etc"`:                   RuleRmOutside,
		"echo $(rm -rf /usr)":                     RuleRmOutside,
		"git push --force origin main":            RuleForcePush,
		"git push -f":                             RuleForcePush,
		"git push origin +main":                   RuleForcePush,
		"git push origin :feature":                RuleForcePush,
		"git -C repo push --delete origin x":      RuleForcePush,
		"git reset --hard HEAD~3":                 RuleDiscardWork,
		"git clean -fdx":                          RuleDiscardWork,
		"git checkout -- .":                       RuleDiscardWork,
		"git restore .":                           RuleDiscardWork,
		"git branch -D old":                       RuleDiscardWork,
		"git stash clear":                         RuleDiscardWork,
		`psql -c "DROP DATABASE app_dev"`:         RuleDatabase,
		"mysql -e 'drop table users'":             RuleDatabase,
		"echo 'TRUNCATE orders;' | psql":          RuleDatabase,
		"dropdb app_dev":                          RuleDatabase,
		"redis-cli FLUSHALL":                      RuleDatabase,
		"docker volume rm pgdata":                 RuleDocker,
		"docker volume prune -f":                  RuleDocker,
		"docker system prune -a --volumes":        RuleDocker,
		"docker compose down -v":                  RuleDocker,
		"docker rm -f postgres-db-16":             RuleDocker,
		"sudo dd if=/dev/zero of=/dev/sda bs=1M":  RuleDisk,
		"mkfs.ext4 /dev/sdb1":                     RuleDisk,
		"chmod -R 777 /":                          RuleRmOutside,
		"FOO=1 nice -n 5 rm --recursive /opt/app": RuleRmOutside,
	} {
		ds := g.Judge(line, here)
		found := false
		for _, d := range ds {
			found = found || d.Rule == rule
		}
		if !found {
			t.Errorf("%q: got %v, want %s", line, ds, rule)
		}
	}
}

// TestTheGuardLetsTheDaysWorkThrough: what an agent runs all day is not
// stopped, or the guard is switched off by the first person it annoys.
func TestTheGuardLetsTheDaysWorkThrough(t *testing.T) {
	g := Guard{Mode: GuardDeny}
	for _, line := range []string{
		"rm -rf build",
		"rm -rf node_modules dist",
		"rm -r ./tmp/cache",
		"rm -rf /tmp/test-run-42",
		"rm old.txt",
		"rm -f ~/notes.txt",
		"git push",
		"git push --force-with-lease origin topic",
		"git push -u origin topic",
		"git reset --soft HEAD~1",
		"git checkout -b topic",
		"git branch -d merged",
		"git stash pop",
		"docker ps -a",
		"docker compose up -d",
		"psql -c 'select * from users'",
		"echo 'drop it like it is hot'",
		"grep -r 'rm -rf' .",
		"dd if=in of=out.img",
		"go test ./...",
	} {
		if ds := g.Judge(line, here); len(ds) > 0 {
			t.Errorf("%q was stopped: %v", line, ds)
		}
	}
}

// TestTheGuardsOwnPatternsComeFirst: guard_allow lets a command through
// whatever the rules say, guard_deny stops one they would not.
func TestTheGuardsOwnPatternsComeFirst(t *testing.T) {
	g := Guard{Mode: GuardAsk, Allow: []string{`^rm -rf /srv/scratch/`}, Deny: []string{`\bterraform destroy\b`}}
	if ds := g.Judge("rm -rf /srv/scratch/run1", here); len(ds) > 0 {
		t.Errorf("guard_allow did not let it through: %v", ds)
	}
	ds := g.Judge("terraform destroy -auto-approve", here)
	if len(ds) != 1 || ds[0].Rule != RuleCustom || !strings.Contains(ds[0].Message, "terraform") {
		t.Errorf("guard_deny: %v", ds)
	}
	if err := (Guard{Mode: "maybe"}).Check(); err == nil {
		t.Error("an unknown mode was accepted")
	}
	if err := (Guard{Deny: []string{"("}}).Check(); err == nil {
		t.Error("a broken pattern was accepted")
	}
}
