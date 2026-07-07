package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/Ratelhhhh/anime-meme-factory/internal/config"
	"github.com/Ratelhhhh/anime-meme-factory/internal/pikabu"
	"github.com/Ratelhhhh/anime-meme-factory/internal/store"
)

// cmdModerate поднимает локальную веб-панель: показывает картинки на модерации
// с кнопками «Одобрить/Отклонить». Ничего не публикуется, пока модератор не
// нажмёт «Одобрить» — одобренные переходят в очередь публикации (tick).
func cmdModerate(cfg config.Config) error {
	if !cfg.Moderation {
		fmt.Println("Внимание: moderation=false в конфиге — панель покажет PENDING, но их там может не быть.")
	}
	statePath := statePathOf(cfg)
	moddir := filepath.Join(filepath.Dir(statePath), "moderation")
	if err := os.MkdirAll(moddir, 0o755); err != nil {
		return err
	}

	srv := &modServer{cfg: cfg, statePath: statePath, moddir: moddir}
	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/img/", srv.handleImg)
	mux.HandleFunc("/approve", srv.handleAction(store.StatusQueued))
	mux.HandleFunc("/reject", srv.handleAction(store.StatusRejected))

	addr := fmt.Sprintf("127.0.0.1:%d", cfg.ModeratePort)
	fmt.Printf("Панель модерации: http://%s  (канал %s)\n", addr, cfg.Channel)
	fmt.Println("Открой ссылку в браузере. Ctrl+C — остановить.")
	return http.ListenAndServe(addr, mux)
}

type modServer struct {
	cfg       config.Config
	statePath string
	moddir    string
	mu        sync.Mutex // сериализует изменения состояния из веб-запросов
}

func (s *modServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	st, err := store.Load(s.statePath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	pending := st.PendingImages()
	if err := indexTmpl.Execute(w, map[string]any{
		"Channel": s.cfg.Channel,
		"Pending": pending,
		"Count":   len(pending),
	}); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

// handleImg отдаёт картинку: берёт из локального кэша, при отсутствии — качает
// с Пикабу и кэширует (чтобы браузер не ходил на хотлинк-защищённый источник).
func (s *modServer) handleImg(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Path[len("/img/"):]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	cache := filepath.Join(s.moddir, idStr)
	if _, err := os.Stat(cache); err != nil {
		st, err := store.Load(s.statePath)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		im, ok := st.ImageByID(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, err := pikabu.Download(im.URL, cache); err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
	}
	http.ServeFile(w, r, cache)
}

// handleAction возвращает обработчик, переводящий картинку в целевой статус
// (queued = одобрить, rejected = отклонить). Отклонённые чистят кэш-файл.
func (s *modServer) handleAction(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			http.Error(w, "bad id", 400)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()

		// Загружаем свежее состояние, чтобы не затереть параллельную запись tick.
		st, err := store.Load(s.statePath)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var ok bool
		if target == store.StatusQueued {
			ok = st.Approve(id)
		} else {
			ok = st.Reject(id)
			os.Remove(filepath.Join(s.moddir, strconv.Itoa(id)))
		}
		if !ok {
			http.Error(w, "не в статусе pending", 409)
			return
		}
		if err := st.Save(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

var indexTmpl = template.Must(template.New("index").Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Модерация — {{.Channel}}</title>
<style>
 :root { color-scheme: dark; }
 body { margin:0; font:15px/1.4 system-ui,sans-serif; background:#12121a; color:#e8e8ef; }
 header { position:sticky; top:0; padding:14px 18px; background:#1b1b28; border-bottom:1px solid #2a2a3c; }
 header b { color:#ff8fd0; }
 .grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(260px,1fr)); gap:14px; padding:18px; }
 .card { background:#1b1b28; border:1px solid #2a2a3c; border-radius:12px; overflow:hidden; display:flex; flex-direction:column; }
 .card img { width:100%; height:260px; object-fit:contain; background:#0c0c12; }
 .meta { padding:8px 10px; font-size:12px; color:#9a9ab0; display:flex; justify-content:space-between; gap:8px; }
 .meta a { color:#8fbaff; text-decoration:none; }
 .btns { display:flex; }
 .btns button { flex:1; border:0; padding:12px; font-size:14px; font-weight:600; cursor:pointer; color:#fff; }
 .ok { background:#1f8a4c; } .ok:hover { background:#25a35a; }
 .no { background:#8a2f2f; } .no:hover { background:#a83636; }
 .empty { padding:60px 18px; text-align:center; color:#9a9ab0; }
</style></head><body>
<header>На модерации: <b id="count">{{.Count}}</b> · канал {{.Channel}} · <a href="/" style="color:#8fbaff">обновить</a></header>
{{if .Pending}}
<div class="grid" id="grid">
 {{range .Pending}}
 <div class="card" id="card-{{.ID}}">
   <img loading="lazy" src="/img/{{.ID}}" alt="id {{.ID}}">
   <div class="meta"><span>id {{.ID}}</span><a href="{{.PostURL}}" target="_blank" rel="noopener">источник ↗</a></div>
   <div class="btns">
     <button class="ok" onclick="act({{.ID}},'approve',this)">✅ Одобрить</button>
     <button class="no" onclick="act({{.ID}},'reject',this)">❌ Отклонить</button>
   </div>
 </div>
 {{end}}
</div>
{{else}}
<div class="empty">Пусто — на модерации ничего нет.<br>Запусти <code>refill</code>, чтобы набрать кандидатов.</div>
{{end}}
<script>
async function act(id, kind, btn){
  btn.parentElement.querySelectorAll('button').forEach(b=>b.disabled=true);
  const res = await fetch('/'+kind+'?id='+id, {method:'POST'});
  if(res.ok || res.status===204){
    const card = document.getElementById('card-'+id);
    card.style.transition='opacity .2s'; card.style.opacity='0';
    setTimeout(()=>card.remove(),200);
    const c = document.getElementById('count'); c.textContent = Math.max(0, +c.textContent-1);
  } else {
    alert('Ошибка: '+res.status+' '+await res.text());
    btn.parentElement.querySelectorAll('button').forEach(b=>b.disabled=false);
  }
}
</script>
</body></html>`))
