package api_test

import (
	"net/http"
	"testing"

	"github.com/tokuhirom/feedla/internal/store"
)

func TestFoldersListAndCreate(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	resp, err := client.Get(apiSrv.URL + "/api/v1/folders")
	if err != nil {
		t.Fatalf("GET /api/v1/folders: %v", err)
	}
	var listed struct {
		Folders []store.Folder `json:"folders"`
	}
	decodeJSON(t, resp, &listed)
	if len(listed.Folders) != 0 {
		t.Fatalf("initial folders = %v, want empty", listed.Folders)
	}

	resp = postJSON(t, client, apiSrv.URL+"/api/v1/folders", map[string]string{"name": "Tech"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create folder status = %d, want 201", resp.StatusCode)
	}
	var created store.Folder
	decodeJSON(t, resp, &created)
	if created.Name != "Tech" || created.ID == 0 {
		t.Fatalf("created folder = %+v, want name=Tech with an id", created)
	}

	// Creating the same name again is get-or-create: same id back, no
	// duplicate row.
	resp = postJSON(t, client, apiSrv.URL+"/api/v1/folders", map[string]string{"name": "Tech"})
	var again store.Folder
	decodeJSON(t, resp, &again)
	if again.ID != created.ID {
		t.Fatalf("re-creating same folder name got id %d, want %d", again.ID, created.ID)
	}

	resp, err = client.Get(apiSrv.URL + "/api/v1/folders")
	if err != nil {
		t.Fatalf("GET /api/v1/folders: %v", err)
	}
	decodeJSON(t, resp, &listed)
	if len(listed.Folders) != 1 {
		t.Fatalf("folders after create = %v, want 1 entry", listed.Folders)
	}
}

func TestCreateFolderMissingName(t *testing.T) {
	apiSrv, _, client := newTestServer(t)

	resp := postJSON(t, client, apiSrv.URL+"/api/v1/folders", map[string]string{"name": "  "})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create folder with blank name status = %d, want 400", resp.StatusCode)
	}
}
