use std::time::{Duration, Instant};

use crossterm::event::{KeyCode, KeyEvent};

use crate::ipc::JsonRpcClient;
use crate::types::{Server, SystemStatus};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Focus {
    Dashboard,
    Servers,
}

pub struct App {
    pub endpoint: String,
    pub status: Option<SystemStatus>,
    pub servers: Vec<Server>,
    pub selected: usize,
    pub focus: Focus,
    pub message: String,
    last_refresh: Instant,
    poll_interval: Duration,
}

impl App {
    pub fn new(endpoint: String) -> Self {
        Self {
            endpoint,
            status: None,
            servers: Vec::new(),
            selected: 0,
            focus: Focus::Dashboard,
            message: "connecting".to_string(),
            last_refresh: Instant::now(),
            poll_interval: Duration::from_millis(4000),
        }
    }

    pub fn set_error(&mut self, msg: String) {
        self.message = msg;
    }

    pub fn refresh(&mut self, client: &mut JsonRpcClient) {
        match client.status() {
            Ok(status) => {
                let ms = status.poll_interval_ms.clamp(1000, 10000);
                self.poll_interval = Duration::from_millis(ms);
                self.status = Some(status);
                self.message = "online".to_string();
            }
            Err(err) => self.message = format!("status: {err}"),
        }
        match client.servers() {
            Ok(list) => {
                self.servers = list.servers;
                if self.selected >= self.servers.len() {
                    self.selected = self.servers.len().saturating_sub(1);
                }
            }
            Err(err) => self.message = format!("servers: {err}"),
        }
        self.last_refresh = Instant::now();
    }

    pub fn needs_refresh(&self) -> bool {
        self.last_refresh.elapsed() >= self.poll_interval
    }

    pub fn handle_key(&mut self, key: KeyEvent, client: &mut JsonRpcClient) -> bool {
        match key.code {
            KeyCode::Char('q') | KeyCode::Esc => return true,
            KeyCode::Tab | KeyCode::Left | KeyCode::Right => self.toggle_focus(),
            KeyCode::Char('r') => self.refresh(client),
            KeyCode::Up | KeyCode::Char('k') => self.move_selection(-1),
            KeyCode::Down | KeyCode::Char('j') => self.move_selection(1),
            KeyCode::Char('s') => self.server_action(client, true),
            KeyCode::Char('x') => self.server_action(client, false),
            _ => {}
        }
        false
    }

    pub fn selected_server(&self) -> Option<&Server> {
        self.servers.get(self.selected)
    }

    pub fn snapshot(&self) -> String {
        let mut out = String::new();
        out.push_str("MCOS Lite Panel\n");
        out.push_str(&format!("Endpoint: {}\n", self.endpoint));
        out.push_str(&format!("Message : {}\n\n", self.message));
        if let Some(st) = &self.status {
            out.push_str(&format!(
                "{} {} | tier {} (detected {}) | panel {}\n",
                or_dash(&st.system_name),
                or_dash(&st.version),
                or_dash(&st.tier),
                or_dash(&st.detected_tier),
                or_dash(&st.panel)
            ));
            out.push_str(&format!(
                "CPU: {} ({}c/{}t) {:.0}% | RAM: {:.1}/{:.1} GiB\n",
                or_dash(&st.cpu.model),
                st.cpu.cores,
                st.cpu.threads,
                st.cpu.usage_pct,
                gib(st
                    .memory
                    .total_bytes
                    .saturating_sub(st.memory.available_bytes)),
                gib(st.memory.total_bytes)
            ));
            out.push_str(&format!(
                "Net: {} internet={} | servers {}/{} | cloudflared={}\n",
                or_dash(&st.net.local_ip),
                st.net.internet,
                st.servers_up,
                st.servers_total,
                or_dash(&st.cloudflared)
            ));
        } else {
            out.push_str("No daemon status yet.\n");
        }
        out.push_str("\nServers:\n");
        if self.servers.is_empty() {
            out.push_str("  (no servers)\n");
        }
        for (i, srv) in self.servers.iter().enumerate() {
            let mark = if i == self.selected { ">" } else { " " };
            out.push_str(&format!(
                "{mark} {} [{}] {} {} Java{} {}MB :{}\n",
                or_dash(&srv.name),
                or_dash(&srv.state),
                or_dash(&srv.software),
                or_dash(&srv.mc_version),
                srv.java_major,
                srv.ram_mb,
                srv.port
            ));
        }
        out
    }

    fn toggle_focus(&mut self) {
        self.focus = match self.focus {
            Focus::Dashboard => Focus::Servers,
            Focus::Servers => Focus::Dashboard,
        };
    }

    fn move_selection(&mut self, delta: isize) {
        if self.focus != Focus::Servers || self.servers.is_empty() {
            return;
        }
        let max = self.servers.len() as isize - 1;
        self.selected = (self.selected as isize + delta).clamp(0, max) as usize;
    }

    fn server_action(&mut self, client: &mut JsonRpcClient, start: bool) {
        if self.focus != Focus::Servers {
            return;
        }
        let Some(id) = self.selected_server().map(|s| s.id.clone()) else {
            return;
        };
        let result = if start {
            client.start_server(&id)
        } else {
            client.stop_server(&id)
        };
        match result {
            Ok(_) => {
                self.message = if start { "starting" } else { "stopping" }.to_string();
                self.refresh(client);
            }
            Err(err) => self.message = err,
        }
    }
}

pub fn gib(bytes: u64) -> f64 {
    bytes as f64 / (1u64 << 30) as f64
}

pub fn or_dash(value: &str) -> &str {
    if value.trim().is_empty() {
        "-"
    } else {
        value
    }
}
