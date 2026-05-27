package dashboard

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:embed templates/*.html
var templateFS embed.FS

var tmpl *template.Template

func init() {
	var err error
	tmpl = template.New("").Funcs(template.FuncMap{
		"ago":        ago,
		"phaseColor": phaseColor,
	})
	err = fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}
		name := filepath.Base(path)
		_, err = tmpl.New(name).Parse(string(b))
		return err
	})
	if err != nil {
		panic("failed to parse templates: " + err.Error())
	}
}

func Render(w io.Writer, name string, data any) error {
	return tmpl.ExecuteTemplate(w, name, data)
}

func ago(t metav1.Time) string {
	d := time.Since(t.Time)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func phaseColor(phase any) string {
	s := fmt.Sprint(phase)
	switch s {
	case "Ready", "Running":
		return "green"
	case "Creating", "Pending":
		return "yellow"
	case "Failed", "Error":
		return "red"
	default:
		return "grey"
	}
}
