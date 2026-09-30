mod app;
mod ipc;
mod types;
mod ui;

use std::env;
use std::error::Error;
use std::io::{self, Stdout};
use std::time::Duration;

use app::App;
use crossterm::event::{self, Event};
use crossterm::execute;
use crossterm::terminal::{
    disable_raw_mode, enable_raw_mode, EnterAlternateScreen, LeaveAlternateScreen,
};
use ipc::JsonRpcClient;
use ratatui::backend::CrosstermBackend;
use ratatui::Terminal;

struct Args {
    connect: String,
    snapshot: bool,
    help: bool,
}

fn default_endpoint() -> String {
    env::var("MCOS_ENDPOINT").unwrap_or_else(|_| {
        if cfg!(windows) {
            "tcp://127.0.0.1:7777".to_string()
        } else {
            "unix:///run/mcos/mcosd.sock".to_string()
        }
    })
}

fn parse_args() -> Result<Args, String> {
    let mut args = Args {
        connect: default_endpoint(),
        snapshot: false,
        help: false,
    };
    let mut it = env::args().skip(1);
    while let Some(arg) = it.next() {
        match arg.as_str() {
            "--connect" => {
                args.connect = it.next().ok_or("--connect requires an endpoint")?;
            }
            "--snapshot" => args.snapshot = true,
            "-h" | "--help" => args.help = true,
            other => return Err(format!("unknown argument {other}")),
        }
    }
    Ok(args)
}

fn main() -> Result<(), Box<dyn Error>> {
    let args = parse_args().map_err(|e| format!("mcos-panel-lite: {e}"))?;
    if args.help {
        print_help();
        return Ok(());
    }

    let mut app = App::new(args.connect.clone());
    match JsonRpcClient::connect(&args.connect) {
        Ok(mut client) => {
            app.refresh(&mut client);
            if args.snapshot {
                println!("{}", app.snapshot());
                return Ok(());
            }
            run_tui(app, client)?;
        }
        Err(err) if args.snapshot => {
            app.set_error(format!("connect: {err}"));
            println!("{}", app.snapshot());
        }
        Err(err) => return Err(format!("connect {}: {err}", args.connect).into()),
    }
    Ok(())
}

fn print_help() {
    println!("mcos-panel-lite - low-tier MCOS TUI");
    println!("Usage: mcos-panel-lite [--connect ENDPOINT] [--snapshot]");
}

fn run_tui(mut app: App, mut client: JsonRpcClient) -> Result<(), Box<dyn Error>> {
    enable_raw_mode()?;
    let mut stdout = io::stdout();
    execute!(stdout, EnterAlternateScreen)?;
    let mut terminal = Terminal::new(CrosstermBackend::new(stdout))?;

    let result = run_loop(&mut terminal, &mut app, &mut client);

    disable_raw_mode()?;
    execute!(terminal.backend_mut(), LeaveAlternateScreen)?;
    terminal.show_cursor()?;
    result
}

fn run_loop(
    terminal: &mut Terminal<CrosstermBackend<Stdout>>,
    app: &mut App,
    client: &mut JsonRpcClient,
) -> Result<(), Box<dyn Error>> {
    loop {
        terminal.draw(|frame| ui::draw(frame, app))?;
        if event::poll(Duration::from_millis(200))? {
            if let Event::Key(key) = event::read()? {
                if app.handle_key(key, client) {
                    return Ok(());
                }
            }
        }
        if app.needs_refresh() {
            app.refresh(client);
        }
    }
}
