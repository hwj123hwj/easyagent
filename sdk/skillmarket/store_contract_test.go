package skillmarket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// This fixture follows the public store's wire fields, independent of our Go
// structs. Encoding SkillItem in a fake store would hide a field-name mismatch.
const storeSkillWire = `{"id":105,"name":"whiteboard-ppt","displayName":"板书 PPT","description":"Whiteboard slides","richDescription":"Details","version":"1.0.0","installCount":30,"sortOrder":200,"tarballSize":12692,"sha256":"abc","ossKey":"skills/package.zip","sectionId":5,"sectionName":"演示与设计","iconUrl":"https://example.test/icon.svg","usageExample":"Make slides","previewImages":["https://example.test/preview.png"],"previewThumbnails":[{"url":"https://example.test/thumb.png","isLong":true}],"exampleFiles":[{"name":"demo.pptx","url":"https://example.test/demo.pptx","size":100,"mimeType":"application/pptx","extension":"pptx"}]}`

func TestOfficialStoreWireFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/skills", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"skills":[` + storeSkillWire + `],"total":1,"page":1}`))
	})
	mux.HandleFunc("GET /api/skills/105", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(storeSkillWire))
	})
	mux.HandleFunc("GET /api/sections", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sections":[{"id":5,"name":"Design","nameI18n":"设计","descriptionI18n":"Artwork","orderIndex":2}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := NewClient(t.TempDir(), WithBaseURL(srv.URL))
	result, err := client.Search(t.Context(), BrowseQuery{})
	if err != nil || len(result.Skills) != 1 {
		t.Fatalf("browse: %+v, %v", result, err)
	}
	detail, err := client.Detail(t.Context(), 105, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []SkillItem{result.Skills[0], detail} {
		if item.DisplayName != "板书 PPT" || item.InstallCount != 30 || item.TarballSize != 12692 || item.SortOrder != 200 || item.SectionID == nil || *item.SectionID != 5 || item.SectionName == nil || *item.SectionName != "演示与设计" {
			t.Fatalf("lost catalog metadata: %+v", item)
		}
		if item.RichDescription == nil || *item.RichDescription != "Details" || item.OSSKey == nil || *item.OSSKey != "skills/package.zip" || item.IconURL == nil || item.UsageExample == nil || len(item.PreviewImages) != 1 || len(item.PreviewThumbnails) != 1 || !item.PreviewThumbnails[0].IsLong || len(item.ExampleFiles) != 1 || item.ExampleFiles[0].MimeType != "application/pptx" {
			t.Fatalf("lost detail metadata: %+v", item)
		}
		encoded, _ := json.Marshal(item)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(encoded, &fields)
		if fields["display_name"] == nil || fields["displayName"] != nil {
			t.Fatalf("changed EasyAgent JSON contract: %s", encoded)
		}
	}
	sections, err := client.Sections(t.Context())
	if err != nil || len(sections) != 1 || sections[0].NameI18n == nil || *sections[0].NameI18n != "设计" || sections[0].Description == nil || sections[0].OrderIndex != 2 {
		t.Fatalf("lost section metadata: %+v, %v", sections, err)
	}
}

func TestStoreWireCompatibilityAndValidation(t *testing.T) {
	var item SkillItem
	if err := json.Unmarshal([]byte(`{"display_name":"Legacy","displayName":"New","install_count":4,"tarball_size":12}`), &item); err != nil || item.DisplayName != "Legacy" || item.InstallCount != 4 || item.TarballSize != 12 {
		t.Fatalf("legacy compatibility: %+v, %v", item, err)
	}
	if err := json.Unmarshal([]byte(`{"installCount":"invalid"}`), &item); err == nil {
		t.Fatal("invalid numeric field accepted")
	}
}
