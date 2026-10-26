# 設定

`lazycred` 不要求先建立設定檔。預設位置是
`$XDG_CONFIG_HOME/lazycred/config.toml`，缺少時使用 `~/.config/lazycred/config.toml`。
相對 XDG 變數不採用；沒有可用使用者目錄時不寫入目前工作目錄。

```toml
bw = "bw"
uv = "uv"
idle_seconds = 300
reveal_seconds = 15
clipboard_seconds = 30

[aliases]
"yubikey:YOUR_SERIAL" = "Travel key"
```

優先序為明確 CLI flags → `LAZYCRED_BW`／`LAZYCRED_UV` → 選定 TOML → 預設值。
`--config` 明確指定不存在或格式錯誤的檔案會報錯，不悄悄回退。`config show --json`
顯示有效偏好，不包含認證資料。timeout 必須大於零，clipboard 上限 600 秒。

只有 explicit setup 才寫 SDK helper 到 `$XDG_CACHE_HOME/lazycred/helpers/<hash>`，
預設 `~/.cache`。這是可重建程式與 uv 環境，不是 vault cache；刪除後重新 setup 即可。
剪貼簿清除 owner 檔只包含隨機 token，沒有被複製的值。修改工具執行檔路徑等於選擇
要執行的可信任 backend；不以 shell 解譯該路徑。

completion 使用 Cobra 原生 generator，例如 `lazycred completion zsh`；自行安裝到
你的 shell completion 路徑。不自動修改 rc、fpath、PATH 或啟用外掛。
