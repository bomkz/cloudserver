// Command example demonstrates jsonstore: save a few versions of a
// document, list its history, load an old version, and print a diff
// between two versions — like `git log` / `git show` / `git diff`.
package maintst

import (
	"fmt"
	"log"

	"github.com/bomkz/cloudserver/jsonstore"
)

type Profile struct {
	Name  string   `json:"name"`
	Bio   string   `json:"bio"`
	Roles []string `json:"roles"`
}

func maintst() {
	store, err := jsonstore.New("./data") // one folder per key lives here
	if err != nil {
		log.Fatal(err)
	}

	p := Profile{Name: "Ada Lovelace", Bio: "Mathematician", Roles: []string{"engineer"}}
	if _, err := store.SaveWithMessage("profiles/ada", p, "initial profile"); err != nil {
		log.Fatal(err)
	}

	p.Bio = "Mathematician and writer"
	if _, err := store.SaveWithMessage("profiles/ada", p, "expand bio"); err != nil {
		log.Fatal(err)
	}

	p.Roles = append(p.Roles, "analyst")
	if _, err := store.SaveWithMessage("profiles/ada", p, "add analyst role"); err != nil {
		log.Fatal(err)
	}

	// git log
	hist, err := store.History("profiles/ada")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("history:")
	for _, v := range hist {
		kind := "diff"
		if v.Snapshot {
			kind = "snapshot"
		}
		fmt.Printf("  v%d [%s] %s - %s\n", v.Version, kind, v.Time.Format("15:04:05"), v.Message)
	}

	// git show v1
	var v1 Profile
	if err := store.LoadVersionInto("profiles/ada", 1, &v1); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nv1 was: %+v\n", v1)

	// git diff v1 v3
	diff, err := store.Diff("profiles/ada", 1, 3)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\ndiff v1 -> v3:")
	fmt.Println(diff)

	// latest
	var latest Profile
	if err := store.Load("profiles/ada", &latest); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("latest: %+v\n", latest)
}
