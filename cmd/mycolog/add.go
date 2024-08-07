package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codesoap/mycolog/store"
)

type addComponentTmplData struct {
	Spores          bool
	Myc             bool
	Spawn           bool
	Grow            bool
	PossibleParents []string
	KnownSpecies    []string
	Today           string
	IsFirst         bool
}

func handleAddComponent(w http.ResponseWriter, r *http.Request) {
	// TODO: split into smaller functions.
	componentType, err := componentTypeFromPath(r.URL.Path)
	if err != nil {
		showError(w, err, "/intro")
		return
	}
	if r.Method == http.MethodPost {
		var id int64
		if r.FormValue("parent1") == "" {
			id, err = addAcquiredComponent(r, componentType)
		} else {
			id, err = addCreatedComponents(r, componentType)
		}
		if err != nil {
			showError(w, err, r.URL.Path)
			return
		}
		http.Redirect(w, r, fmt.Sprint("/component/", id), http.StatusSeeOther)
		return
	} else {
		// TODO: Enable passing prefilled parents via query parameter.
		possibleParents, err := getPossibleParentIdentifiers()
		if err != nil {
			showError(w, err, r.URL.Path)
			return
		}
		knownSpecies, err := db.GetAllSpecies()
		if err != nil {
			showError(w, err, r.URL.Path)
			return
		}
		componentsPresent, err := db.ComponentsPresent()
		if err != nil {
			showError(w, err, r.URL.Path)
			return
		}
		w.Header().Add("Content-Type", "text/html")
		data := addComponentTmplData{
			Spores:          componentType == store.TypeSpores,
			Myc:             componentType == store.TypeMycelium,
			Spawn:           componentType == store.TypeSpawn,
			Grow:            componentType == store.TypeGrow,
			PossibleParents: possibleParents,
			KnownSpecies:    knownSpecies,
			Today:           time.Now().Format("2006-01-02"),
			IsFirst:         !componentsPresent,
		}
		if err := tmpls["add"].Execute(w, data); err != nil {
			log.Println(err.Error())
		}
	}
}

// getPossibleParentIdentifiers finds components, that are not already
// gone. They are returned as a string each, which includes the ID,
// token and type.
func getPossibleParentIdentifiers() ([]string, error) {
	gone := false
	// FIXME: What if the parents were already marked gone?
	components, err := db.FindComponents(store.ComponentFilter{Gone: &gone})
	if err != nil {
		return nil, err
	}
	identifiers := make([]string, len(components))
	for i, component := range components {
		switch component.Type {
		case store.TypeSpores:
			identifiers[i] = fmt.Sprintf("Spores %s (#%d)", component.Token, component.ID)
		case store.TypeMycelium:
			identifiers[i] = fmt.Sprintf("Mycelium %s (#%d)", component.Token, component.ID)
		case store.TypeSpawn:
			identifiers[i] = fmt.Sprintf("Spawn %s (#%d)", component.Token, component.ID)
		case store.TypeGrow:
			identifiers[i] = fmt.Sprintf("Grow %s (#%d)", component.Token, component.ID)
		}
	}
	return identifiers, nil
}

func addCreatedComponents(r *http.Request, ct store.ComponentType) (int64, error) {
	createdAt, err := time.Parse("2006-01-02", r.FormValue("createdAt"))
	if err != nil {
		return 0, fmt.Errorf("received invalid createdAt value")
	}
	amount, err := strconv.Atoi(r.FormValue("amount"))
	if err != nil || amount < 1 {
		return 0, fmt.Errorf("received invalid amount value")
	}
	parents, species, err := getParents(r, createdAt)
	if err != nil {
		return 0, err
	}
	component := store.Component{
		Type:      ct,
		Species:   species,
		CreatedAt: createdAt,
		Notes:     r.FormValue("notes"),
	}
	var firstID int64
	for i := 0; i < amount; i++ {
		id, _, err := db.AddComponent(component)
		if err != nil {
			return 0, err
		}
		err = db.SetParents(id, parents)
		if err != nil {
			return 0, err
		}
		if i == 0 {
			firstID = id
		}
	}
	return firstID, nil
}

func addAcquiredComponent(r *http.Request, ct store.ComponentType) (int64, error) {
	species := strings.TrimSpace(r.FormValue("species"))
	createdAt, err := time.Parse("2006-01-02", r.FormValue("createdAt"))
	if err != nil {
		return 0, fmt.Errorf("received invalid createdAt value")
	} else if species == "" {
		return 0, fmt.Errorf("species missing")
	}
	component := store.Component{
		Type:      ct,
		Species:   species,
		CreatedAt: createdAt,
		Notes:     r.FormValue("notes"),
	}
	id, _, err := db.AddComponent(component)
	return id, err
}

// getParents gets the parents from the parentX fields, eliminating
// duplicates. It is checked if all parents have the same species and
// were created before createdAt. An error will be returned otherwise.
// Will also return an error, if no parent is specified.
func getParents(r *http.Request, createdAt time.Time) (parents []int64, species string, err error) {
	parentSet := make(map[int64]bool)
	for i := 1; i < 7; i++ {
		formValue := strings.TrimSpace(r.FormValue(fmt.Sprint("parent", i)))
		if len(formValue) == 0 {
			continue
		}
		idString := strings.TrimLeft(reComponentID.FindString(formValue), "#")
		var id int64
		id, err = strconv.ParseInt(idString, 10, 64)
		if err != nil {
			return
		}
		parentSet[id] = true
	}
	for k := range parentSet {
		parents = append(parents, k)
	}
	if len(parents) == 0 {
		err = fmt.Errorf("no parents given")
		return
	}
	components, err := db.GetComponents(parents)
	if err != nil {
		return
	}
	species = components[0].Species
	for _, component := range components {
		if component.Species != species {
			err = fmt.Errorf("not all parents have the same species")
			return
		} else if component.CreatedAt.Sub(createdAt) >= 0 {
			err = fmt.Errorf("a parent was not created before the new component")
			return
		}
	}
	return
}

func componentTypeFromPath(path string) (store.ComponentType, error) {
	pathSplit := strings.Split(path, "-")
	if len(pathSplit) < 2 {
		return "", fmt.Errorf("invalid URL to add a component")
	}
	switch pathSplit[len(pathSplit)-1] {
	case "spores":
		return store.TypeSpores, nil
	case "mycelium":
		return store.TypeMycelium, nil
	case "spawn":
		return store.TypeSpawn, nil
	case "grow":
		return store.TypeGrow, nil
	}
	return "", fmt.Errorf("invalid URL to add a component")
}
