package system

// M17: lệnh của site trong lồng là systemd-run chạy bằng root với -p User= của
// site; luật lệnh root của M28 chỉ nhận đúng dạng internal/cage dựng.

import (
	"errors"
	"testing"
)

func cageRunArgs(extra ...string) []string {
	args := []string{
		"--quiet", "--wait", "--pipe", "--expand-environment=no", "--service-type=oneshot",
		"--unit=tpan-cage-a_example_com_7653e4-run-0123456789abcdef",
		"-p", "User=tp_a_example_com_7653e4", "-p", "Group=tp_a_example_com_7653e4",
		"-p", "NoNewPrivileges=yes", "-p", "TimeoutStartSec=31", "-E", "LC_ALL=C",
	}
	args = append(args, extra...)
	// Lệnh sau "--" chạm sâu vào thư mục site: chạy bằng user của site.
	return append(args, "--", "wp", "plugin", "list", "--path=/var/www/a.example.com/public/wp-content/x")
}

func TestRootZoneNhanSystemdRunCuaLong(t *testing.T) {
	ok := []Cmd{
		{Name: "systemd-run", Args: cageRunArgs()},
		{Name: "systemd-run", Args: cageRunArgs("--working-directory=/var/www/a.example.com")},
		{Name: "systemd-run", Args: cageRunArgs("--working-directory=/tmp")},
	}
	for _, c := range ok {
		if err := checkRootZone("/var/www", c); err != nil {
			t.Errorf("dạng của lồng bị từ chối: %v", err)
		}
	}
}

func TestRootZoneTuChoiSystemdRunLech(t *testing.T) {
	base := cageRunArgs()
	replace := func(old, new string) []string {
		out := append([]string(nil), base...)
		for i, a := range out {
			if a == old {
				out[i] = new
				return out
			}
		}
		t.Fatalf("không có %q trong argv mẫu", old)
		return nil
	}
	drop := func(n ...string) []string {
		var out []string
	next:
		for i := 0; i < len(base); i++ {
			for _, x := range n {
				if base[i] == x {
					if base[i] == "-p" {
						i++
					}
					continue next
				}
			}
			out = append(out, base[i])
		}
		return out
	}
	cases := map[string]Cmd{
		"User=root":                  {Name: "systemd-run", Args: replace("User=tp_a_example_com_7653e4", "User=root")},
		"thiếu NoNewPrivileges":      {Name: "systemd-run", Args: replace("NoNewPrivileges=yes", "NoNewPrivileges=no")},
		"thuộc tính lạ":              {Name: "systemd-run", Args: replace("TimeoutStartSec=31", "ExecStartPre=/var/www/a.example.com/x")},
		"tên unit lạ":                {Name: "systemd-run", Args: replace("--unit=tpan-cage-a_example_com_7653e4-run-0123456789abcdef", "--unit=ssh")},
		"cờ lạ":                      {Name: "systemd-run", Args: replace("--pipe", "--scope")},
		"thư mục làm việc sâu":       {Name: "systemd-run", Args: cageRunArgs("--working-directory=/var/www/a.example.com/public")},
		"Dir trong vùng":             {Name: "systemd-run", Args: base, Dir: "/var/www/a.example.com"},
		"Env chạm vùng":              {Name: "systemd-run", Args: base, Env: []string{"X=/var/www/a.example.com/x"}},
		"không có User":              {Name: "systemd-run", Args: drop("User=tp_a_example_com_7653e4")},
		"không có Group":             {Name: "systemd-run", Args: drop("Group=tp_a_example_com_7653e4")},
		"không có --unit":            {Name: "systemd-run", Args: drop("--unit=tpan-cage-a_example_com_7653e4-run-0123456789abcdef")},
		"Group của site khác":        {Name: "systemd-run", Args: replace("Group=tp_a_example_com_7653e4", "Group=tp_b_example_com_1a2b3c")},
		"User khác slug unit":        {Name: "systemd-run", Args: replace("User=tp_a_example_com_7653e4", "User=tp_b_example_com_1a2b3c")},
		"User lặp hai lần":           {Name: "systemd-run", Args: append(append([]string(nil), base[:6]...), append([]string{"-p", "User=tp_a_example_com_7653e4"}, base[6:]...)...)},
		"thư mục làm việc tương đối": {Name: "systemd-run", Args: cageRunArgs("--working-directory=var/www/a.example.com/x")},
		"thư mục làm việc có ..":     {Name: "systemd-run", Args: cageRunArgs("--working-directory=/var/www/a.example.com/../b.example.com")},
	}
	for name, c := range cases {
		if err := checkRootZone("/var/www", c); !errors.Is(err, ErrRootZoneCommand) {
			t.Errorf("%s: muốn từ chối, có %v", name, err)
		}
	}
}
