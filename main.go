package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Service represents a homelab service tile
type Service struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Position    int    `json:"position"`
}

// Store manages services in a JSON file
type Store struct {
	mu       sync.RWMutex
	filePath string
	services []Service
	nextID   int
}

func NewStore(path string) *Store {
	s := &Store{filePath: path}
	s.load()
	return s
}

func (s *Store) load() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		s.services = []Service{}
		s.nextID = 1
		return
	}
	json.Unmarshal(data, &s.services)
	maxID := 0
	for _, svc := range s.services {
		if svc.ID > maxID {
			maxID = svc.ID
		}
	}
	s.nextID = maxID + 1
}

func (s *Store) save() error {
	data, err := json.MarshalIndent(s.services, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

func (s *Store) All() []Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sorted := make([]Service, len(s.services))
	copy(sorted, s.services)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Position != sorted[j].Position {
			return sorted[i].Position < sorted[j].Position
		}
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

func (s *Store) Find(id int) (Service, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, svc := range s.services {
		if svc.ID == id {
			return svc, true
		}
	}
	return Service{}, false
}

func (s *Store) Create(svc Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc.ID = s.nextID
	s.nextID++
	s.services = append(s.services, svc)
	return s.save()
}

func (s *Store) Update(svc Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.services {
		if existing.ID == svc.ID {
			s.services[i] = svc
			return s.save()
		}
	}
	return fmt.Errorf("service %d not found", svc.ID)
}

func (s *Store) Delete(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, svc := range s.services {
		if svc.ID == id {
			s.services = append(s.services[:i], s.services[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("service %d not found", id)
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.services)
}

// Template data structs
type IndexData struct {
	Services []Service
	Count    int
	Time     string
	Notice   string
}

type FormData struct {
	Service Service
	Title   string
	Subtitle string
	IsEdit  bool
	Errors  []string
}

var tmpl *template.Template

func init() {
	tmpl = template.Must(template.New("").Parse(layoutTmpl + indexTmpl + newTmpl + editTmpl + formTmpl))
}

func main() {
	port := "3003"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	dataFile := "services.json"
	if f := os.Getenv("DATA_FILE"); f != "" {
		dataFile = f
	}

	store := NewStore(dataFile)

	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		notice := r.URL.Query().Get("notice")
		data := IndexData{
			Services: store.All(),
			Count:    store.Count(),
			Time:     time.Now().Format("15:04"),
			Notice:   notice,
		}
		tmpl.ExecuteTemplate(w, "layout", map[string]interface{}{
			"Title":   "HomeLab",
			"Content": data,
			"Page":    "index",
		})
	})

	mux.HandleFunc("GET /new", func(w http.ResponseWriter, r *http.Request) {
		data := FormData{
			Title:    "Nowy serwis",
			Subtitle: "Dodaj nowy serwis do dashboardu",
		}
		tmpl.ExecuteTemplate(w, "layout", map[string]interface{}{
			"Title":   "Nowy serwis — HomeLab",
			"Content": data,
			"Page":    "new",
		})
	})

	mux.HandleFunc("POST /create", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		pos, _ := strconv.Atoi(r.FormValue("position"))
		svc := Service{
			Name:        r.FormValue("name"),
			URL:         r.FormValue("url"),
			Description: r.FormValue("description"),
			Icon:        r.FormValue("icon"),
			Position:    pos,
		}

		var errors []string
		if svc.Name == "" {
			errors = append(errors, "Nazwa jest wymagana")
		}
		if svc.URL == "" {
			errors = append(errors, "URL jest wymagany")
		}
		if len(errors) > 0 {
			data := FormData{Service: svc, Title: "Nowy serwis", Subtitle: "Dodaj nowy serwis do dashboardu", Errors: errors}
			tmpl.ExecuteTemplate(w, "layout", map[string]interface{}{
				"Title": "Nowy serwis — HomeLab", "Content": data, "Page": "new",
			})
			return
		}

		store.Create(svc)
		http.Redirect(w, r, "/?notice=Serwis+zosta%C5%82+dodany.", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /edit/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		svc, ok := store.Find(id)
		if !ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		data := FormData{
			Service:  svc,
			Title:    "Edytuj serwis",
			Subtitle: svc.Name,
			IsEdit:   true,
		}
		tmpl.ExecuteTemplate(w, "layout", map[string]interface{}{
			"Title":   "Edytuj serwis — HomeLab",
			"Content": data,
			"Page":    "edit",
		})
	})

	mux.HandleFunc("POST /update/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		r.ParseForm()
		pos, _ := strconv.Atoi(r.FormValue("position"))
		svc := Service{
			ID:          id,
			Name:        r.FormValue("name"),
			URL:         r.FormValue("url"),
			Description: r.FormValue("description"),
			Icon:        r.FormValue("icon"),
			Position:    pos,
		}

		var errors []string
		if svc.Name == "" {
			errors = append(errors, "Nazwa jest wymagana")
		}
		if svc.URL == "" {
			errors = append(errors, "URL jest wymagany")
		}
		if len(errors) > 0 {
			data := FormData{Service: svc, Title: "Edytuj serwis", Subtitle: svc.Name, IsEdit: true, Errors: errors}
			tmpl.ExecuteTemplate(w, "layout", map[string]interface{}{
				"Title": "Edytuj serwis — HomeLab", "Content": data, "Page": "edit",
			})
			return
		}

		store.Update(svc)
		http.Redirect(w, r, "/?notice=Serwis+zosta%C5%82+zaktualizowany.", http.StatusSeeOther)
	})

	mux.HandleFunc("POST /delete/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		store.Delete(id)
		http.Redirect(w, r, "/?notice=Serwis+zosta%C5%82+usuni%C4%99ty.", http.StatusSeeOther)
	})

	addr := "0.0.0.0:" + port
	log.Printf("HomeLab Dashboard running on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// ─── Templates ──────────────────────────────────────────────────────────────

const layoutTmpl = `{{define "layout"}}<!DOCTYPE html>
<html>
<head>
  <title>{{.Title}}</title>
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta charset="utf-8">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&display=swap" rel="stylesheet">
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

    body {
      font-family: 'Inter', system-ui, sans-serif;
      background: #0a0a0f;
      color: #fff;
      min-height: 100vh;
    }

    body::before {
      content: "";
      position: fixed;
      inset: 0;
      z-index: -2;
      background: radial-gradient(ellipse at 15% 50%, rgba(59,130,246,0.12) 0%, transparent 50%),
                  radial-gradient(ellipse at 85% 20%, rgba(139,92,246,0.10) 0%, transparent 50%),
                  radial-gradient(ellipse at 50% 90%, rgba(6,182,212,0.08) 0%, transparent 50%);
    }

    body::after {
      content: "";
      position: fixed;
      inset: 0;
      z-index: -1;
      opacity: 0.03;
      background-image: url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E");
      pointer-events: none;
    }

    .glass-card {
      background: linear-gradient(135deg, rgba(255,255,255,0.05) 0%, rgba(255,255,255,0.02) 100%);
      backdrop-filter: blur(12px);
      -webkit-backdrop-filter: blur(12px);
      border: 1px solid rgba(255,255,255,0.08);
      box-shadow: 0 8px 32px rgba(0,0,0,0.3), inset 0 1px 0 rgba(255,255,255,0.05);
    }

    .glass-card:hover {
      background: linear-gradient(135deg, rgba(255,255,255,0.08) 0%, rgba(255,255,255,0.04) 100%);
      border-color: rgba(255,255,255,0.15);
      box-shadow: 0 12px 40px rgba(0,0,0,0.4), inset 0 1px 0 rgba(255,255,255,0.08);
    }

    .glass-input {
      background: rgba(255,255,255,0.05);
      backdrop-filter: blur(8px);
      border: 1px solid rgba(255,255,255,0.1);
      transition: all 0.3s ease;
      color: #fff;
    }

    .glass-input:focus {
      background: rgba(255,255,255,0.08);
      border-color: rgba(59,130,246,0.5);
      box-shadow: 0 0 20px rgba(59,130,246,0.15);
      outline: none;
    }

    .glass-input::placeholder { color: #4b5563; }

    .btn-primary {
      position: relative;
      overflow: hidden;
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      background: linear-gradient(to right, #2563eb, #7c3aed);
      color: #fff;
      padding: 0.625rem 1.25rem;
      border-radius: 0.75rem;
      border: none;
      cursor: pointer;
      transition: all 0.3s;
      font-size: 0.875rem;
      font-weight: 500;
      font-family: inherit;
      text-decoration: none;
      box-shadow: 0 4px 15px rgba(37,99,235,0.2);
    }

    .btn-primary:hover {
      background: linear-gradient(to right, #3b82f6, #8b5cf6);
      box-shadow: 0 4px 20px rgba(37,99,235,0.3);
    }

    .btn-primary::after {
      content: "";
      position: absolute;
      inset: 0;
      background: linear-gradient(105deg, transparent 40%, rgba(255,255,255,0.12) 45%, rgba(255,255,255,0.12) 55%, transparent 60%);
      transform: translateX(-100%);
      transition: transform 0.5s ease;
    }

    .btn-primary:hover::after { transform: translateX(100%); }

    .icon-glow { transition: filter 0.3s ease, transform 0.3s ease; }
    .tile:hover .icon-glow { filter: drop-shadow(0 0 8px rgba(59,130,246,0.5)); transform: scale(1.1); }

    .status-dot {
      width: 8px; height: 8px; border-radius: 50%;
      background: #22c55e;
      box-shadow: 0 0 6px rgba(34,197,94,0.6);
      animation: pulse-dot 2s ease-in-out infinite;
    }

    @keyframes pulse-dot {
      0%, 100% { opacity: 1; box-shadow: 0 0 6px rgba(34,197,94,0.6); }
      50% { opacity: 0.6; box-shadow: 0 0 12px rgba(34,197,94,0.8); }
    }

    .flash-notice { animation: slide-down 0.4s ease-out; }

    @keyframes slide-down {
      from { opacity: 0; transform: translateY(-12px); }
      to { opacity: 1; transform: translateY(0); }
    }

    .emoji-picker-wrap { position: relative; }
    .emoji-selected {
      width: 100%; border-radius: 0.75rem; padding: 0.75rem 1rem;
      text-align: center; font-size: 1.5rem; cursor: pointer;
      display: flex; align-items: center; justify-content: center; gap: 0.5rem;
    }
    .emoji-selected .hint { font-size: 0.75rem; color: #6b7280; }
    .emoji-grid {
      display: none; position: absolute; top: 100%; left: 0;
      width: 16rem; margin-top: 0.5rem; padding: 0.75rem;
      border-radius: 0.75rem; max-height: 18rem; overflow-y: auto;
      grid-template-columns: repeat(6, 1fr); gap: 0.25rem; z-index: 50;
    }
    .emoji-grid.open { display: grid; }
    .emoji-grid button {
      font-size: 1.25rem; padding: 0.375rem; border: none; border-radius: 0.5rem;
      background: transparent; cursor: pointer; transition: background 0.15s;
      line-height: 1;
    }
    .emoji-grid button:hover { background: rgba(255,255,255,0.1); }

    .action-btn {
      width: 2rem; height: 2rem; border-radius: 0.5rem;
      background: rgba(255,255,255,0.05);
      border: none; cursor: pointer;
      display: flex; align-items: center; justify-content: center;
      transition: background 0.2s;
    }

    .action-btn:hover { background: rgba(255,255,255,0.1); }
    .action-btn.delete:hover { background: rgba(239,68,68,0.2); }

    .actions { opacity: 0; transition: opacity 0.2s; display: flex; gap: 0.375rem; }
    .tile:hover .actions { opacity: 1; }

    .tile { transition: transform 0.3s, box-shadow 0.3s; }
    .tile:hover { transform: scale(1.02); }

    a { color: inherit; text-decoration: none; }

    .grid { display: grid; grid-template-columns: repeat(1, 1fr); gap: 1.25rem; }
    @media (min-width: 640px) { .grid { grid-template-columns: repeat(2, 1fr); } }
    @media (min-width: 1024px) { .grid { grid-template-columns: repeat(3, 1fr); } }
  </style>
</head>
<body>
  <header style="padding: 3rem 0 2rem; text-align: center;">
    <div style="display: inline-flex; align-items: center; gap: 0.75rem; margin-bottom: 0.5rem;">
      <div style="width:2.5rem;height:2.5rem;border-radius:0.75rem;background:linear-gradient(135deg,#3b82f6,#7c3aed);display:flex;align-items:center;justify-content:center;box-shadow:0 4px 15px rgba(59,130,246,0.2);">
        <svg style="width:1.25rem;height:1.25rem;color:#fff;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01"/></svg>
      </div>
      <h1 style="font-size:1.875rem;font-weight:700;background:linear-gradient(to right,#fff,#e5e7eb,#9ca3af);-webkit-background-clip:text;-webkit-text-fill-color:transparent;">HomeLab</h1>
    </div>
    <p style="color:#6b7280;font-size:0.75rem;letter-spacing:0.15em;text-transform:uppercase;">Dashboard</p>
  </header>

  <main style="max-width:72rem;margin:0 auto;padding:0 1.5rem 4rem;">
    {{if eq .Page "index"}}{{template "index" .Content}}{{end}}
    {{if eq .Page "new"}}{{template "new" .Content}}{{end}}
    {{if eq .Page "edit"}}{{template "edit" .Content}}{{end}}
  </main>

  <footer style="text-align:center;padding-bottom:2rem;color:#4b5563;font-size:0.75rem;letter-spacing:0.05em;">
    {{if eq .Page "index"}}{{.Content.Time}} &middot; {{.Content.Count}} serwisów{{end}}
  </footer>
  <script>
  (function() {
    var emojis = [
      ['Serwery', '🖥️','💻','🖲️','🗄️','📡','🔌','⚡','🏠'],
      ['Media', '🎬','🎵','📺','🎮','📷','📸','🎧','📻'],
      ['Sieci', '🌐','🔒','🛡️','🔑','🔗','📶','🧭','🕸️'],
      ['Dane', '💾','📁','📂','💿','🗃️','📊','📈','🔍'],
      ['Narzędzia', '🔧','⚙️','🛠️','🧰','📐','🧪','🤖','🐳'],
      ['Inne', '☁️','🏗️','📝','✉️','📮','🗺️','💡','🚀']
    ];
    var grid = document.getElementById('emoji-grid');
    var toggle = document.getElementById('emoji-toggle');
    if (!grid || !toggle) return;
    toggle.addEventListener('click', function(e) {
      e.preventDefault();
      e.stopPropagation();
      grid.classList.toggle('open');
    });
    emojis.forEach(function(cat) {
      var label = document.createElement('div');
      label.style.cssText = 'grid-column:1/-1;font-size:0.625rem;color:#6b7280;text-transform:uppercase;letter-spacing:0.1em;padding:0.25rem 0;margin-top:0.25rem;';
      label.textContent = cat[0];
      grid.appendChild(label);
      for (var i = 1; i < cat.length; i++) {
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.textContent = cat[i];
        btn.onclick = (function(e) { return function() {
          document.getElementById('emoji-input').value = e;
          document.getElementById('emoji-preview').textContent = e;
          grid.classList.remove('open');
        }})(cat[i]);
        grid.appendChild(btn);
      }
    });
    document.addEventListener('click', function(e) {
      if (!e.target.closest('.emoji-picker-wrap')) {
        grid.classList.remove('open');
      }
    });
  })();
  </script>
</body>
</html>{{end}}`

const indexTmpl = `{{define "index"}}
{{if .Notice}}
<div class="flash-notice glass-card" style="border-radius:0.75rem;padding:1rem;margin-bottom:2rem;color:#6ee7b7;font-size:0.875rem;display:flex;align-items:center;gap:0.75rem;">
  <svg style="width:1.25rem;height:1.25rem;flex-shrink:0;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
  {{.Notice}}
</div>
{{end}}

<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:2rem;">
  <p style="color:#6b7280;font-size:0.875rem;">{{.Count}} aktywnych serwisów</p>
  <a href="/new" class="btn-primary">
    <svg style="width:1rem;height:1rem;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4"/></svg>
    Dodaj serwis
  </a>
</div>

<div class="grid">
  {{range .Services}}
  <div class="tile glass-card" style="border-radius:1rem;padding:1.5rem;position:relative;cursor:default;">
    <div class="actions" style="position:absolute;top:1rem;right:1rem;">
      <a href="/edit/{{.ID}}" class="action-btn">
        <svg style="width:0.875rem;height:0.875rem;color:#9ca3af;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg>
      </a>
      <form method="POST" action="/delete/{{.ID}}" style="margin:0;" onsubmit="return confirm('Na pewno usunąć {{.Name}}?')">
        <button type="submit" class="action-btn delete">
          <svg style="width:0.875rem;height:0.875rem;color:#9ca3af;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
        </button>
      </form>
    </div>

    <a href="{{.URL}}" target="_blank" rel="noopener" style="display:block;">
      <div style="display:flex;align-items:flex-start;gap:1rem;">
        <div style="width:3.5rem;height:3.5rem;border-radius:0.75rem;background:linear-gradient(135deg,rgba(255,255,255,0.1),rgba(255,255,255,0.05));display:flex;align-items:center;justify-content:center;flex-shrink:0;">
          <span class="icon-glow" style="font-size:1.875rem;">{{if .Icon}}{{.Icon}}{{else}}🖥️{{end}}</span>
        </div>
        <div style="min-width:0;flex:1;">
          <div style="display:flex;align-items:center;gap:0.5rem;margin-bottom:0.25rem;">
            <h2 style="font-size:1.125rem;font-weight:600;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;">{{.Name}}</h2>
            <div class="status-dot" style="flex-shrink:0;"></div>
          </div>
          <p style="color:#9ca3af;font-size:0.875rem;line-height:1.5;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;">{{.Description}}</p>
          <p style="color:#4b5563;font-size:0.75rem;margin-top:0.5rem;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;font-family:monospace;">{{.URL}}</p>
        </div>
      </div>
    </a>
  </div>
  {{end}}
</div>

{{if eq .Count 0}}
<div style="text-align:center;margin-top:6rem;">
  <div style="width:5rem;height:5rem;border-radius:1rem;background:linear-gradient(135deg,rgba(59,130,246,0.2),rgba(139,92,246,0.2));display:flex;align-items:center;justify-content:center;margin:0 auto 1.5rem;">
    <svg style="width:2.5rem;height:2.5rem;color:#6b7280;" fill="none" stroke="currentColor" stroke-width="1.5" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2"/></svg>
  </div>
  <p style="color:#9ca3af;font-size:1.125rem;font-weight:500;">Brak serwisów</p>
  <p style="color:#4b5563;margin-top:0.25rem;font-size:0.875rem;">Dodaj pierwszy serwis, aby rozpocząć.</p>
  <a href="/new" class="btn-primary" style="margin-top:1.5rem;padding:0.75rem 1.5rem;">
    <svg style="width:1.25rem;height:1.25rem;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4"/></svg>
    Dodaj serwis
  </a>
</div>
{{end}}
{{end}}`

const formTmpl = `{{define "form"}}
<form method="POST" action="{{if .IsEdit}}/update/{{.Service.ID}}{{else}}/create{{end}}" style="max-width:28rem;margin:0 auto;display:flex;flex-direction:column;gap:1.25rem;">
  {{if .Errors}}
  <div class="flash-notice glass-card" style="border-radius:0.75rem;padding:1rem;color:#fca5a5;font-size:0.875rem;">
    <ul style="list-style:none;display:flex;flex-direction:column;gap:0.25rem;">
      {{range .Errors}}<li style="display:flex;align-items:center;gap:0.5rem;">
        <svg style="width:1rem;height:1rem;flex-shrink:0;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
        {{.}}
      </li>{{end}}
    </ul>
  </div>
  {{end}}

  <div>
    <label style="display:block;font-size:0.875rem;font-weight:500;color:#9ca3af;margin-bottom:0.375rem;">Nazwa</label>
    <input type="text" name="name" value="{{.Service.Name}}" placeholder="np. Proxmox" class="glass-input" style="width:100%;border-radius:0.75rem;padding:0.75rem 1rem;font-family:inherit;font-size:inherit;">
  </div>

  <div>
    <label style="display:block;font-size:0.875rem;font-weight:500;color:#9ca3af;margin-bottom:0.375rem;">URL</label>
    <input type="url" name="url" value="{{.Service.URL}}" placeholder="https://192.168.1.100:8006" class="glass-input" style="width:100%;border-radius:0.75rem;padding:0.75rem 1rem;font-family:monospace;font-size:0.875rem;">
  </div>

  <div>
    <label style="display:block;font-size:0.875rem;font-weight:500;color:#9ca3af;margin-bottom:0.375rem;">Opis</label>
    <input type="text" name="description" value="{{.Service.Description}}" placeholder="Wirtualizacja i kontenery" class="glass-input" style="width:100%;border-radius:0.75rem;padding:0.75rem 1rem;font-family:inherit;font-size:inherit;">
  </div>

  <div style="display:grid;grid-template-columns:1fr 1fr;gap:1rem;">
    <div>
      <label style="display:block;font-size:0.875rem;font-weight:500;color:#9ca3af;margin-bottom:0.375rem;">Ikona</label>
      <div class="emoji-picker-wrap">
        <input type="hidden" name="icon" id="emoji-input" value="{{.Service.Icon}}">
        <button type="button" id="emoji-toggle" class="emoji-selected glass-input">
          <span id="emoji-preview" style="font-size:1.75rem;line-height:1;">{{if .Service.Icon}}{{.Service.Icon}}{{else}}🖥️{{end}}</span>
          <span class="hint">zmień</span>
        </button>
        <div id="emoji-grid" class="emoji-grid glass-card"></div>
      </div>
    </div>
    <div>
      <label style="display:block;font-size:0.875rem;font-weight:500;color:#9ca3af;margin-bottom:0.375rem;">Pozycja</label>
      <input type="number" name="position" value="{{.Service.Position}}" placeholder="1" class="glass-input" style="width:100%;border-radius:0.75rem;padding:0.75rem 1rem;font-family:inherit;font-size:inherit;">
    </div>
  </div>

  <div style="display:flex;gap:0.75rem;padding-top:0.5rem;">
    <button type="submit" class="btn-primary" style="flex:1;justify-content:center;padding:0.75rem;">{{if .IsEdit}}Zaktualizuj{{else}}Utwórz{{end}}</button>
    <a href="/" class="glass-card" style="flex:1;text-align:center;padding:0.75rem;border-radius:0.75rem;color:#9ca3af;transition:color 0.2s;display:flex;align-items:center;justify-content:center;">Anuluj</a>
  </div>
</form>
{{end}}`

const newTmpl = `{{define "new"}}
<div style="margin-top:2rem;">
  <div style="text-align:center;margin-bottom:2.5rem;">
    <div style="width:3.5rem;height:3.5rem;border-radius:0.75rem;background:linear-gradient(135deg,rgba(59,130,246,0.2),rgba(139,92,246,0.2));display:flex;align-items:center;justify-content:center;margin:0 auto 1rem;">
      <svg style="width:1.75rem;height:1.75rem;color:#60a5fa;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4v16m8-8H4"/></svg>
    </div>
    <h1 style="font-size:1.5rem;font-weight:700;">{{.Title}}</h1>
    <p style="color:#6b7280;font-size:0.875rem;margin-top:0.25rem;">{{.Subtitle}}</p>
  </div>
  {{template "form" .}}
</div>
{{end}}`

const editTmpl = `{{define "edit"}}
<div style="margin-top:2rem;">
  <div style="text-align:center;margin-bottom:2.5rem;">
    <div style="width:3.5rem;height:3.5rem;border-radius:0.75rem;background:linear-gradient(135deg,rgba(245,158,11,0.2),rgba(249,115,22,0.2));display:flex;align-items:center;justify-content:center;margin:0 auto 1rem;">
      <svg style="width:1.75rem;height:1.75rem;color:#fbbf24;" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/></svg>
    </div>
    <h1 style="font-size:1.5rem;font-weight:700;">{{.Title}}</h1>
    <p style="color:#6b7280;font-size:0.875rem;margin-top:0.25rem;">{{.Subtitle}}</p>
  </div>
  {{template "form" .}}
</div>
{{end}}`
