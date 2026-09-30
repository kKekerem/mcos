use ratatui::prelude::*;
use ratatui::widgets::{Block, Borders, BorderType, Gauge, Paragraph, Wrap};

use crate::app::{gib, or_dash, App, Focus};

const BG: Color = Color::Rgb(15, 11, 10);
const SURFACE: Color = Color::Rgb(31, 29, 27);
const ACCENT: Color = Color::Rgb(218, 160, 255);
const ORANGE: Color = Color::Rgb(255, 154, 86);
const GREEN: Color = Color::Rgb(88, 214, 141);
const RED: Color = Color::Rgb(255, 104, 104);
const MUTED: Color = Color::Rgb(145, 138, 132);

pub fn draw(frame: &mut Frame<'_>, app: &App) {
    let area = frame.area();
    frame.render_widget(Block::default().style(Style::default().bg(BG)), area);
    let chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([Constraint::Length(28), Constraint::Min(30)])
        .split(area);
    draw_sidebar(frame, chunks[0], app);
    match app.focus {
        Focus::Dashboard => draw_dashboard(frame, chunks[1], app),
        Focus::Servers => draw_servers(frame, chunks[1], app),
    }
}

fn draw_sidebar(frame: &mut Frame<'_>, area: Rect, app: &App) {
    let active_dashboard = marker(app.focus == Focus::Dashboard);
    let active_servers = marker(app.focus == Focus::Servers);
    let tier = app.status.as_ref().map(|s| or_dash(&s.tier)).unwrap_or("-");
    let body = vec![
        Line::from(vec![Span::styled("MCOS Lite", title_style())]),
        Line::raw(""),
        Line::from(vec![Span::styled(
            format!("{active_dashboard} Dashboard"),
            menu_style(app.focus == Focus::Dashboard),
        )]),
        Line::from(vec![Span::styled(
            format!("{active_servers} Servers"),
            menu_style(app.focus == Focus::Servers),
        )]),
        Line::raw(""),
        Line::from(vec![
            Span::styled("Tier: ", Style::default().fg(MUTED)),
            Span::styled(tier, Style::default().fg(ORANGE)),
        ]),
        Line::from(vec![
            Span::styled("IPC: ", Style::default().fg(MUTED)),
            Span::styled(
                shorten(&app.endpoint, 18),
                Style::default().fg(Color::White),
            ),
        ]),
        Line::raw(""),
        Line::from(Span::styled(
            "tab switch  r refresh",
            Style::default().fg(MUTED),
        )),
        Line::from(Span::styled(
            "s start     x stop",
            Style::default().fg(MUTED),
        )),
        Line::from(Span::styled("q quit", Style::default().fg(MUTED))),
        Line::raw(""),
        Line::from(Span::styled(&app.message, Style::default().fg(GREEN))),
    ];
    let block = Block::default()
        .title(" control ")
        .borders(Borders::ALL)
        .border_type(BorderType::Rounded)
        .border_style(Style::default().fg(ACCENT))
        .style(Style::default().bg(SURFACE));
    frame.render_widget(
        Paragraph::new(body).block(block).wrap(Wrap { trim: true }),
        area,
    );
}

fn draw_dashboard(frame: &mut Frame<'_>, area: Rect, app: &App) {
    let outer = Block::default()
        .title(" dashboard ")
        .borders(Borders::ALL)
        .border_type(BorderType::Rounded)
        .border_style(Style::default().fg(ACCENT))
        .style(Style::default().bg(BG));
    let inner = outer.inner(area);
    frame.render_widget(outer, area);

    let rows = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Length(8),
            Constraint::Length(6),
            Constraint::Min(6),
        ])
        .margin(1)
        .split(inner);

    let Some(st) = &app.status else {
        frame.render_widget(Paragraph::new("Waiting for daemon status..."), rows[0]);
        return;
    };

    let summary = vec![
        Line::from(vec![
            Span::styled(or_dash(&st.system_name), title_style()),
            Span::raw("  "),
            Span::styled(or_dash(&st.version), Style::default().fg(MUTED)),
        ]),
        Line::from(format!(
            "tier {} (detected {}) | panel {} | poll {}ms",
            or_dash(&st.tier),
            or_dash(&st.detected_tier),
            or_dash(&st.panel),
            st.poll_interval_ms
        )),
        Line::from(format!(
            "servers {}/{} | java {:?} | peers {}",
            st.servers_up, st.servers_total, st.java_versions, st.peers_online
        )),
        Line::from(format!(
            "network {} internet={} cloudflared={}",
            or_dash(&st.net.local_ip),
            st.net.internet,
            or_dash(&st.cloudflared)
        )),
    ];
    frame.render_widget(
        Paragraph::new(summary)
            .block(card("System"))
            .style(Style::default().fg(Color::White)),
        rows[0],
    );

    let gauges = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([Constraint::Percentage(50), Constraint::Percentage(50)])
        .split(rows[1]);
    frame.render_widget(gauge("CPU", st.cpu.usage_pct, ORANGE), gauges[0]);
    frame.render_widget(gauge("RAM", st.memory.usage_pct, ACCENT), gauges[1]);

    let details = vec![
        Line::from(format!(
            "CPU: {} ({} cores / {} threads)",
            or_dash(&st.cpu.model),
            st.cpu.cores,
            st.cpu.threads
        )),
        Line::from(format!(
            "RAM: {:.1} GiB total, {:.1} GiB free",
            gib(st.memory.total_bytes),
            gib(st.memory.available_bytes)
        )),
        Line::from(format!(
            "Features: gpu={} analytics={} full-perf={} cluster={} cloudflared={}",
            st.gpu_monitor_on,
            st.advanced_analytics_on,
            st.full_performance_on,
            st.cluster_on,
            st.cloudflared_on
        )),
        Line::from(format!("Host: {}", or_dash(&st.net.hostname))),
    ];
    frame.render_widget(
        Paragraph::new(details).block(card("Adaptive policy")),
        rows[2],
    );
}

fn draw_servers(frame: &mut Frame<'_>, area: Rect, app: &App) {
    let outer = Block::default()
        .title(" servers ")
        .borders(Borders::ALL)
        .border_type(BorderType::Rounded)
        .border_style(Style::default().fg(ACCENT))
        .style(Style::default().bg(BG));
    let inner = outer.inner(area);
    frame.render_widget(outer, area);

    let mut lines = Vec::new();
    if app.servers.is_empty() {
        lines.push(Line::from(Span::styled(
            "No servers yet. Use the rich panel wizard or mcosctl create.",
            Style::default().fg(MUTED),
        )));
    }
    for (idx, srv) in app.servers.iter().enumerate() {
        let selected = idx == app.selected;
        let color = state_color(&srv.state);
        lines.push(Line::from(vec![
            Span::styled(marker(selected), Style::default().fg(ACCENT)),
            Span::styled(format!(" {} ", or_dash(&srv.name)), menu_style(selected)),
            Span::styled(
                format!("[{}] ", or_dash(&srv.state)),
                Style::default().fg(color),
            ),
            Span::styled(
                format!(
                    "{} {} Java{} {}MB :{} players {}",
                    or_dash(&srv.software),
                    or_dash(&srv.mc_version),
                    srv.java_major,
                    srv.ram_mb,
                    srv.port,
                    srv.players
                ),
                Style::default().fg(Color::White),
            ),
        ]));
        if !srv.last_log.trim().is_empty() {
            lines.push(Line::from(Span::styled(
                format!("    {}", shorten(&srv.last_log, 88)),
                Style::default().fg(MUTED),
            )));
        }
    }
    frame.render_widget(
        Paragraph::new(lines)
            .block(card("Managed Minecraft servers"))
            .wrap(Wrap { trim: false }),
        inner.inner(Margin {
            vertical: 1,
            horizontal: 2,
        }),
    );
}

fn marker(active: bool) -> &'static str {
    if active {
        "➜"
    } else {
        " "
    }
}

fn menu_style(active: bool) -> Style {
    if active {
        Style::default().fg(ACCENT).add_modifier(Modifier::BOLD)
    } else {
        Style::default().fg(Color::White)
    }
}

fn title_style() -> Style {
    Style::default().fg(ORANGE).add_modifier(Modifier::BOLD)
}

fn card(title: &'static str) -> Block<'static> {
    Block::default()
        .title(title)
        .borders(Borders::ALL)
        .border_type(BorderType::Rounded)
        .border_style(Style::default().fg(SURFACE))
}

fn gauge(title: &'static str, value: f64, color: Color) -> Gauge<'static> {
    Gauge::default()
        .block(card(title))
        .gauge_style(Style::default().fg(color).bg(SURFACE))
        .ratio((value / 100.0).clamp(0.0, 1.0))
        .label(format!("{value:.0}%"))
}

fn state_color(state: &str) -> Color {
    match state {
        "running" | "starting" => GREEN,
        "error" => RED,
        _ => MUTED,
    }
}

fn shorten(value: &str, max: usize) -> String {
    let mut out = String::new();
    for (idx, ch) in value.chars().enumerate() {
        if idx + 1 >= max {
            out.push('~');
            return out;
        }
        out.push(ch);
    }
    out
}
