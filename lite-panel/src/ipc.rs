use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpStream;

#[cfg(unix)]
use std::os::unix::net::UnixStream;

use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::types::{OKResult, ServerListResult, SystemStatus};

enum Connection {
    Tcp(TcpStream),
    #[cfg(unix)]
    Unix(UnixStream),
}

impl Read for Connection {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        match self {
            Connection::Tcp(s) => s.read(buf),
            #[cfg(unix)]
            Connection::Unix(s) => s.read(buf),
        }
    }
}

impl Write for Connection {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        match self {
            Connection::Tcp(s) => s.write(buf),
            #[cfg(unix)]
            Connection::Unix(s) => s.write(buf),
        }
    }

    fn flush(&mut self) -> std::io::Result<()> {
        match self {
            Connection::Tcp(s) => s.flush(),
            #[cfg(unix)]
            Connection::Unix(s) => s.flush(),
        }
    }
}

pub struct JsonRpcClient {
    reader: BufReader<Connection>,
    next_id: i64,
}

#[derive(Serialize)]
struct Request {
    jsonrpc: &'static str,
    id: i64,
    method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    params: Option<Value>,
}

#[derive(Deserialize)]
struct Response {
    #[allow(dead_code)]
    jsonrpc: String,
    #[allow(dead_code)]
    id: i64,
    result: Option<Value>,
    error: Option<RpcError>,
}

#[derive(Debug, Deserialize)]
struct RpcError {
    code: i64,
    message: String,
}

impl JsonRpcClient {
    pub fn connect(endpoint: &str) -> Result<Self, String> {
        let conn = match parse_endpoint(endpoint)? {
            Endpoint::Tcp(addr) => {
                Connection::Tcp(TcpStream::connect(addr).map_err(|e| e.to_string())?)
            }
            Endpoint::Unix(path) => open_unix(&path)?,
        };
        Ok(Self {
            reader: BufReader::new(conn),
            next_id: 0,
        })
    }

    pub fn status(&mut self) -> Result<SystemStatus, String> {
        self.call("system.status", None)
    }

    pub fn servers(&mut self) -> Result<ServerListResult, String> {
        self.call("server.list", None)
    }

    pub fn start_server(&mut self, id: &str) -> Result<OKResult, String> {
        self.call("server.start", Some(json!({ "id": id })))
    }

    pub fn stop_server(&mut self, id: &str) -> Result<OKResult, String> {
        self.call("server.stop", Some(json!({ "id": id })))
    }

    fn call<T: DeserializeOwned>(
        &mut self,
        method: &str,
        params: Option<Value>,
    ) -> Result<T, String> {
        self.next_id += 1;
        let req = Request {
            jsonrpc: "2.0",
            id: self.next_id,
            method: method.to_string(),
            params,
        };
        let mut payload = serde_json::to_vec(&req).map_err(|e| e.to_string())?;
        payload.push(b'\n');
        self.reader
            .get_mut()
            .write_all(&payload)
            .map_err(|e| format!("write: {e}"))?;

        let mut line = String::new();
        self.reader
            .read_line(&mut line)
            .map_err(|e| format!("read: {e}"))?;
        if line.is_empty() {
            return Err("daemon closed the connection".to_string());
        }
        let resp: Response = serde_json::from_str(&line).map_err(|e| format!("decode: {e}"))?;
        if let Some(err) = resp.error {
            return Err(format!("rpc {}: {}", err.code, err.message));
        }
        let result = resp.result.ok_or("missing result")?;
        serde_json::from_value(result).map_err(|e| format!("decode result: {e}"))
    }
}

enum Endpoint {
    Tcp(String),
    Unix(String),
}

fn parse_endpoint(endpoint: &str) -> Result<Endpoint, String> {
    if let Some(addr) = endpoint.strip_prefix("tcp://") {
        if addr.trim().is_empty() {
            return Err("tcp endpoint is empty".to_string());
        }
        return Ok(Endpoint::Tcp(addr.to_string()));
    }
    if let Some(path) = endpoint.strip_prefix("unix://") {
        if path.trim().is_empty() {
            return Err("unix endpoint is empty".to_string());
        }
        return Ok(Endpoint::Unix(path.to_string()));
    }
    Err(format!(
        "unsupported endpoint {endpoint}; use tcp:// or unix://"
    ))
}

#[cfg(unix)]
fn open_unix(path: &str) -> Result<Connection, String> {
    UnixStream::connect(path)
        .map(Connection::Unix)
        .map_err(|e| e.to_string())
}

#[cfg(not(unix))]
fn open_unix(_path: &str) -> Result<Connection, String> {
    Err("unix sockets are not available on this host".to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_tcp_endpoint() {
        match parse_endpoint("tcp://127.0.0.1:7777").unwrap() {
            Endpoint::Tcp(addr) => assert_eq!(addr, "127.0.0.1:7777"),
            Endpoint::Unix(_) => panic!("expected tcp"),
        }
    }

    #[test]
    fn rejects_bad_endpoint() {
        assert!(parse_endpoint("http://localhost").is_err());
        assert!(parse_endpoint("tcp://").is_err());
    }
}
