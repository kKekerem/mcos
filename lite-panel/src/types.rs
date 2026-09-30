use serde::Deserialize;

#[derive(Clone, Debug, Default, Deserialize)]
pub struct SystemStatus {
    #[serde(default, rename = "systemName")]
    pub system_name: String,
    #[serde(default)]
    pub version: String,
    #[serde(default)]
    pub tier: String,
    #[serde(default, rename = "detectedTier")]
    pub detected_tier: String,
    #[serde(default)]
    pub panel: String,
    #[serde(default, rename = "pollIntervalMs")]
    pub poll_interval_ms: u64,
    #[serde(default)]
    pub cpu: CPUInfo,
    #[serde(default)]
    pub memory: MemInfo,
    #[serde(default)]
    pub net: NetStatus,
    #[serde(default, rename = "javaVersions")]
    pub java_versions: Vec<i32>,
    #[serde(default, rename = "serversTotal")]
    pub servers_total: usize,
    #[serde(default, rename = "serversUp")]
    pub servers_up: usize,
    #[serde(default)]
    pub cloudflared: String,
    #[serde(default, rename = "peersOnline")]
    pub peers_online: usize,
    #[serde(default, rename = "gpuMonitorOn")]
    pub gpu_monitor_on: bool,
    #[serde(default, rename = "advancedAnalyticsOn")]
    pub advanced_analytics_on: bool,
    #[serde(default, rename = "fullPerformanceOn")]
    pub full_performance_on: bool,
    #[serde(default, rename = "clusterOn")]
    pub cluster_on: bool,
    #[serde(default, rename = "cloudflaredOn")]
    pub cloudflared_on: bool,
}

#[derive(Clone, Debug, Default, Deserialize)]
pub struct CPUInfo {
    #[serde(default)]
    pub model: String,
    #[serde(default)]
    pub cores: usize,
    #[serde(default)]
    pub threads: usize,
    #[serde(default, rename = "usagePct")]
    pub usage_pct: f64,
}

#[derive(Clone, Debug, Default, Deserialize)]
pub struct MemInfo {
    #[serde(default, rename = "totalBytes")]
    pub total_bytes: u64,
    #[serde(default, rename = "availableBytes")]
    pub available_bytes: u64,
    #[serde(default, rename = "usagePct")]
    pub usage_pct: f64,
}

#[derive(Clone, Debug, Default, Deserialize)]
pub struct NetStatus {
    #[serde(default, rename = "localIP")]
    pub local_ip: String,
    #[serde(default)]
    pub internet: bool,
    #[serde(default)]
    pub hostname: String,
}

#[derive(Clone, Debug, Default, Deserialize)]
pub struct Server {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub software: String,
    #[serde(default, rename = "mcVersion")]
    pub mc_version: String,
    #[serde(default, rename = "javaMajor")]
    pub java_major: i32,
    #[serde(default, rename = "ramMB")]
    pub ram_mb: i32,
    #[serde(default)]
    pub port: i32,
    #[serde(default)]
    pub state: String,
    #[serde(default)]
    pub players: i32,
    #[serde(default, rename = "lastLog")]
    pub last_log: String,
}

#[derive(Debug, Deserialize)]
pub struct ServerListResult {
    #[serde(default)]
    pub servers: Vec<Server>,
}

#[derive(Debug, Deserialize)]
pub struct OKResult {
    #[allow(dead_code)]
    pub ok: bool,
    #[allow(dead_code)]
    pub message: Option<String>,
}
