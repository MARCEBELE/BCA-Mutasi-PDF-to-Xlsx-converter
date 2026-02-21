"""
BCA Statement Converter - GUI
"""

import tkinter as tk
from tkinter import filedialog, messagebox
import subprocess, threading, os, sys, glob, re


# When frozen by PyInstaller (--onefile), all bundled files are extracted to
# sys._MEIPASS at startup — that is where bca-converter.exe and pdf_to_txt.py
# live at runtime.  Using sys.executable (the EXE on disk) would look in the
# wrong folder and require the user to keep extra files next to the EXE.
if getattr(sys, 'frozen', False):
    SCRIPT_DIR = sys._MEIPASS          # temp extraction folder (bundle contents)
else:
    SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))

EXE_PATH = os.path.join(SCRIPT_DIR, "bca-converter.exe")

BG       = "#0f1117"
SURFACE  = "#1a1d27"
SURFACE2 = "#22263a"
ACCENT   = "#3b82f6"
ACCENT2  = "#10b981"
WARN     = "#f59e0b"
ERR      = "#ef4444"
TEXT     = "#e2e8f0"
DIM      = "#64748b"

FONT_BODY  = ("Consolas", 10)
FONT_HEAD  = ("Segoe UI", 11, "bold")
FONT_SMALL = ("Consolas", 9)
FONT_TITLE = ("Segoe UI", 15, "bold")


def find_statements(paths):
    result = []
    for p in paths:
        if os.path.isdir(p):
            for ext in ("*.pdf", "*.txt"):
                result.extend(glob.glob(os.path.join(p, "**", ext), recursive=True))
        elif os.path.isfile(p) and p.lower().endswith((".pdf", ".txt")):
            result.append(p)
    seen, out = set(), []
    for f in result:
        k = os.path.normcase(os.path.abspath(f))
        if k not in seen:
            seen.add(k)
            out.append(f)
    return out


def strip_ansi(text):
    return re.sub(r'\x1b\[[0-9;]*m', '', text)


def run_converter(files, log_cb, done_cb):
    """Pass ALL files to the exe in one single call."""
    if not os.path.exists(EXE_PATH):
        log_cb(f"ERROR: bca-converter.exe not found at:\n  {EXE_PATH}\nBuild it first with BUILD.bat\n", "err")
        done_cb(0, len(files))
        return

    log_cb(f"Running converter on {len(files)} file(s)...\n", "dim")

    try:
        flags = subprocess.CREATE_NO_WINDOW if sys.platform == "win32" else 0
        proc = subprocess.Popen(
            [EXE_PATH] + files,           # ALL files in one call
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            encoding="utf-8",
            errors="replace",
            creationflags=flags
        )

        ok = fail = 0
        for raw_line in proc.stdout:
            line = strip_ansi(raw_line).rstrip()
            if not line:
                continue
            if line.startswith("OK:"):
                ok += 1
                log_cb(line + "\n", "ok")
            elif line.startswith("ERROR:"):
                fail += 1
                log_cb(line + "\n", "err")
            elif line.startswith("DONE:"):
                log_cb("\n" + line + "\n", "ok")
            elif "failed" in line.lower() and "0 failed" not in line.lower():
                log_cb(line + "\n", "warn")
            else:
                log_cb(line + "\n", "dim")

        proc.wait()
        done_cb(ok, fail)

    except Exception as e:
        log_cb(f"ERROR: {e}\n", "err")
        done_cb(0, len(files))


class App(tk.Tk):
    def __init__(self):
        super().__init__()
        self.title("BCA Statement Converter")
        self.configure(bg=BG)
        self.resizable(True, True)
        self.minsize(700, 540)
        self._files   = []
        self._running = False
        self._build_ui()
        self._center()
        self._enable_drop()

    def _build_ui(self):
        # Header
        hdr = tk.Frame(self, bg=SURFACE, pady=14)
        hdr.pack(fill="x")
        tk.Label(hdr, text="BCA Statement Converter",
                 font=FONT_TITLE, fg=TEXT, bg=SURFACE).pack(side="left", padx=20)
        tk.Label(hdr, text="PDF · TXT → Excel",
                 font=("Segoe UI", 10), fg=DIM, bg=SURFACE).pack(side="left", padx=4)

        # Drop zone
        drop_frame = tk.Frame(self, bg=BG, pady=12, padx=16)
        drop_frame.pack(fill="x")
        self.drop_lbl = tk.Label(
            drop_frame,
            text="📂  Drag & drop files or folders here, or use the buttons below",
            font=("Segoe UI", 10), fg=DIM, bg=SURFACE2,
            relief="flat", pady=22, padx=16, cursor="hand2"
        )
        self.drop_lbl.pack(fill="x", ipady=4)
        self.drop_lbl.bind("<Button-1>", lambda e: self._pick_files())

        # Buttons
        btn_row = tk.Frame(self, bg=BG, padx=16, pady=4)
        btn_row.pack(fill="x")
        self._btn("Add Files",   self._pick_files,  btn_row, ACCENT).pack(side="left", padx=(0,6))
        self._btn("Add Folder",  self._pick_folder, btn_row, ACCENT).pack(side="left", padx=(0,6))
        self._btn("Clear",       self._clear_files, btn_row, SURFACE2).pack(side="left")
        self.run_btn = self._btn("▶  Convert All", self._run, btn_row, ACCENT2)
        self.run_btn.pack(side="right")

        # File count label
        lf = tk.Frame(self, bg=BG, padx=16)
        lf.pack(fill="x")
        self.file_count = tk.Label(lf, text="No files added",
                                   font=FONT_SMALL, fg=DIM, bg=BG, anchor="w")
        self.file_count.pack(fill="x", pady=(4, 2))

        # File listbox
        lb_wrap = tk.Frame(lf, bg=SURFACE2, height=120)
        lb_wrap.pack(fill="x")
        lb_wrap.pack_propagate(False)
        self.file_lb = tk.Listbox(
            lb_wrap, font=FONT_SMALL, fg=TEXT, bg=SURFACE2,
            selectbackground=ACCENT, selectforeground=TEXT,
            relief="flat", bd=0, highlightthickness=0, activestyle="none"
        )
        sb = tk.Scrollbar(lb_wrap, orient="vertical", command=self.file_lb.yview)
        self.file_lb.configure(yscrollcommand=sb.set)
        sb.pack(side="right", fill="y")
        self.file_lb.pack(fill="both", expand=True, padx=6, pady=4)
        self.file_lb.bind("<Button-3>", self._remove_selected)

        # Separator
        tk.Frame(self, bg=SURFACE2, height=1).pack(fill="x", padx=16, pady=8)

        # Log label + clear button
        log_hdr = tk.Frame(self, bg=BG, padx=16)
        log_hdr.pack(fill="x")
        tk.Label(log_hdr, text="Output log", font=FONT_HEAD, fg=DIM, bg=BG).pack(side="left")
        self._btn("Clear log", self._clear_log, log_hdr, SURFACE2, small=True).pack(side="right")

        # Log text
        log_wrap = tk.Frame(self, bg=BG, padx=16, pady=4)
        log_wrap.pack(fill="both", expand=True)
        self.log = tk.Text(
            log_wrap, font=FONT_BODY, fg=TEXT, bg=SURFACE,
            relief="flat", bd=0, highlightthickness=0,
            wrap="word", state="disabled", padx=10, pady=8
        )
        log_sb = tk.Scrollbar(log_wrap, orient="vertical", command=self.log.yview)
        self.log.configure(yscrollcommand=log_sb.set)
        log_sb.pack(side="right", fill="y")
        self.log.pack(fill="both", expand=True)
        self.log.tag_config("ok",   foreground=ACCENT2)
        self.log.tag_config("err",  foreground=ERR)
        self.log.tag_config("dim",  foreground=DIM)
        self.log.tag_config("warn", foreground=WARN)

        # Status bar
        self.status = tk.Label(self, text="Ready", font=FONT_SMALL,
                               fg=DIM, bg=SURFACE, anchor="w", padx=16, pady=6)
        self.status.pack(fill="x", side="bottom")

    def _btn(self, text, cmd, parent, color, small=False):
        f   = ("Segoe UI", 9)        if small else ("Segoe UI", 10, "bold")
        pad = (8, 4)                  if small else (14, 8)
        b   = tk.Button(parent, text=text, command=cmd,
                        font=f, fg=TEXT, bg=color,
                        activebackground=color, activeforeground=TEXT,
                        relief="flat", bd=0,
                        padx=pad[0], pady=pad[1], cursor="hand2")
        lighter = self._lighten(color)
        b.bind("<Enter>", lambda e: b.configure(bg=lighter))
        b.bind("<Leave>", lambda e: b.configure(bg=color))
        return b

    @staticmethod
    def _lighten(h):
        h = h.lstrip("#")
        r, g, b = int(h[:2],16), int(h[2:4],16), int(h[4:],16)
        return "#{:02x}{:02x}{:02x}".format(min(255,r+25), min(255,g+25), min(255,b+25))

    # ── File management ──────────────────────────────────────────────────────
    def _pick_files(self):
        paths = filedialog.askopenfilenames(
            title="Select BCA Statement Files",
            filetypes=[("BCA statements","*.pdf *.txt"),("All files","*.*")]
        )
        if paths:
            self._add_files(list(paths))

    def _pick_folder(self):
        folder = filedialog.askdirectory(title="Select Folder with BCA Statements")
        if folder:
            found = find_statements([folder])
            if not found:
                messagebox.showinfo("No files found", "No .pdf or .txt files found in that folder.")
                return
            self._add_files(found)

    def _add_files(self, paths):
        new = find_statements(paths)
        existing = set(os.path.normcase(os.path.abspath(f)) for f in self._files)
        added = 0
        for f in new:
            k = os.path.normcase(os.path.abspath(f))
            if k not in existing:
                self._files.append(f)
                existing.add(k)
                added += 1
        self._refresh_list()
        if added:
            self._log(f"Added {added} file(s)\n", "dim")

    def _remove_selected(self, event):
        for i in reversed(self.file_lb.curselection()):
            self._files.pop(i)
        self._refresh_list()

    def _clear_files(self):
        self._files.clear()
        self._refresh_list()

    def _refresh_list(self):
        self.file_lb.delete(0, "end")
        for f in self._files:
            ext  = os.path.splitext(f)[1].upper()
            icon = "PDF" if ext == ".PDF" else "TXT"
            self.file_lb.insert("end", f"  [{icon}]  {os.path.basename(f)}   —   {os.path.dirname(f)}")
        n = len(self._files)
        self.file_count.config(
            text=f"{n} file(s) queued  (right-click to remove)" if n else "No files added"
        )

    # ── Log ─────────────────────────────────────────────────────────────────
    def _log(self, msg, tag=""):
        self.log.configure(state="normal")
        self.log.insert("end", msg, tag)
        self.log.see("end")
        self.log.configure(state="disabled")

    def _clear_log(self):
        self.log.configure(state="normal")
        self.log.delete("1.0","end")
        self.log.configure(state="disabled")

    # ── Convert ─────────────────────────────────────────────────────────────
    def _run(self):
        if self._running:
            return
        if not self._files:
            messagebox.showwarning("No files", "Add at least one file first.")
            return

        self._running = True
        self.run_btn.configure(text="⏳  Running...", state="disabled")
        self.status.configure(text=f"Converting {len(self._files)} file(s)...", fg=ACCENT)
        sep = "─" * 54
        self._log(f"\n{sep}\nStarting {len(self._files)} file(s)\n{sep}\n", "dim")

        snapshot = list(self._files)

        def done(ok, fail):
            self._running = False
            self.run_btn.configure(text="▶  Convert All", state="normal")
            colour = ACCENT2 if fail == 0 else WARN if ok > 0 else ERR
            msg = f"Done — {ok} succeeded, {fail} failed"
            self.status.configure(text=msg, fg=colour)
            self._log(f"\n{sep}\n{msg}\n{sep}\n", "ok" if fail == 0 else "warn")

        def log_safe(msg, tag=""):
            self.after(0, lambda m=msg, t=tag: self._log(m, t))

        def done_safe(ok, fail):
            self.after(0, lambda: done(ok, fail))

        threading.Thread(
            target=run_converter,
            args=(snapshot, log_safe, done_safe),
            daemon=True
        ).start()

    # ── Drag & drop ─────────────────────────────────────────────────────────
    def _enable_drop(self):
        """
        Enable Windows drag-and-drop using the 'windnd' package.
        windnd hooks WM_DROPFILES at the right level for Tkinter windows.

        If windnd is not installed, shows a one-time tip in the log and falls
        back gracefully (Add Files / Add Folder buttons still work fine).

        Install: pip install windnd
        """
        if sys.platform != "win32":
            return
        try:
            import windnd
            windnd.hook_dropfiles(self, func=self._on_windnd_drop)
        except ImportError:
            self.after(500, lambda: self._log(
                "TIP: Install 'windnd' for drag-and-drop support:\n"
                "     pip install windnd\n"
                "     Then restart the app.\n", "warn"))
        except Exception:
            pass

    def _on_windnd_drop(self, files):
        """Called by windnd with a list of bytes paths."""
        paths = []
        for f in files:
            if isinstance(f, bytes):
                try:    paths.append(f.decode('utf-8'))
                except: paths.append(f.decode('mbcs', errors='replace'))
            else:
                paths.append(str(f))
        if paths:
            self._add_files(paths)

    def _center(self):
        self.update_idletasks()
        w, h = 720, 580
        sw, sh = self.winfo_screenwidth(), self.winfo_screenheight()
        self.geometry(f"{w}x{h}+{(sw-w)//2}+{(sh-h)//2}")


if __name__ == "__main__":
    App().mainloop()
