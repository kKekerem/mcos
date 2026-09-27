package catalog

// Dependency is one dependency a Modrinth version declares.
//
// Ayrı dosyada: catalog.go başka özelliklerle paylaşılıyor. Performans paketi
// (internal/perfpack) bir modu kurmadan önce ZORUNLU bağımlılığı olup
// olmadığına bakar: bağımlılığı eksik bir mod sunucuyu açılışta düşürür.
type Dependency struct {
	ProjectID string `json:"project_id,omitempty"`
	VersionID string `json:"version_id,omitempty"`
	FileName  string `json:"file_name,omitempty"`
	// DependencyType: "required" | "optional" | "incompatible" | "embedded".
	DependencyType string `json:"dependency_type"`
}

// Required returns the project ids this version cannot run without.
//
// "embedded" bağımlılık jar'ın İÇİNDE gelir (ör. VMP'nin "-all" jar'ı);
// ayrıca kurulması gerekmez, sayılmaz.
func (v Version) Required() []string {
	// Modrinth bağımlılığı bazen YALNIZCA sürüm kimliğiyle bildirir; o da
	// sayılır, yoksa bağımlılığı olan mod "bağımsız" sanılıp kurulurdu.
	var out []string
	for _, d := range v.Dependencies {
		if d.DependencyType != "required" {
			continue
		}
		switch {
		case d.ProjectID != "":
			out = append(out, d.ProjectID)
		case d.VersionID != "":
			out = append(out, "sürüm:"+d.VersionID)
		case d.FileName != "":
			out = append(out, d.FileName)
		}
	}
	return out
}
