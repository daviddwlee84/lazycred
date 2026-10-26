# lazycred

A terminal credential workbench over **YubiKey Manager and Bitwarden**.
See the state, choose a source, inspect an item, and copy or act without leaving
your terminal. lazycred is an integration layer; your existing tools continue
to own their keys, vaults and cryptographic operations.

## 安裝與啟動

需要 Go **1.26+**，首版支援 macOS／Linux：

```sh
go install github.com/daviddwlee84/lazycred/cmd/lazycred@v0.1.0
lazycred --version
lazycred --demo                 # 假資料；不讀裝置、vault 或系統剪貼簿
lazycred
```

Go 安裝位置由 `go env GOBIN GOPATH` 決定，請將相應 bin 加入 PATH。
更新既有 Go 安裝：

```sh
go install github.com/daviddwlee84/lazycred/cmd/lazycred@latest
```

`@latest` 選擇最新發行 tag，不一定是 main 的最新 commit。首版刻意沿用原生
Go 安裝／更新，不建立自更新器或 Homebrew 發行管線；移動過的執行檔需確認你更新的是
實際使用的那一份。版本、Help、completion 皆可離線使用。

從原始碼執行：

```sh
make build
./bin/lazycred --demo
go install ./cmd/lazycred
```

## 準備資料來源

**YubiKey**：安裝 `uv`、`ykman` 與平台所需的智慧卡／FIDO 權限，然後明確準備官方 SDK 環境：

```sh
lazycred setup yubikey
```

這會透過 uv 準備固定版本的 `yubikey-manager`／PyOTP，可能下載依賴；不改金鑰設定。
平常啟動與讀取使用 offline 執行，不自動安裝。Linux 依發行版準備 `pcscd`、CCID
及 udev 權限。Go binary 內含薄 Python bridge，不需依賴 checkout 路徑。

**Bitwarden**：安裝官方 `bw` 並使用它登入。lazycred 可在 TUI 遮罩輸入主密碼解鎖，
也可沿用你已建立的 `BW_SESSION`。原生 `bw unlock` 會讓舊 CLI session keys 失效，
解鎖畫面會提示這個影響；不代為登入或改 server 設定。

原始 `bw list items` 回應可能含整個 vault 的秘密；僅在記憶體處理並轉成 metadata，
不保存磁碟 cache。選定項目才呈現欄位，秘密預設遮罩；metadata JSON 始終遮罩。

## 瀏覽與操作

| Key | 操作 |
|---|---|
| `↑↓` / `j,k`、`gg/G` | 導覽目前 pane，第一／最後一列 |
| Tab / Shift+Tab、`h/l` | 切換來源、項目、詳細資料焦點 |
| Enter | Overview 開啟來源；項目開啟詳細資料 |
| `/` | 本來源 filter；All items 可跨可讀來源搜尋 |
| `s` / `c` | 選來源／分類 |
| `y` / `v` | 明確選擇複製欄位／暫時揭露秘密 |
| `u` / `L` | 解鎖目前來源或應用／清除本工具的認證狀態 |
| `a` / `d` / `p` | 適用時新增 TOTP、刪除確切憑證、變更 PIN／密碼 |
| `t` | 將個人 Bitwarden login 的標準 TOTP 複製到指定硬體 |
| `e` | 匯出公開憑證、公鑰或 fingerprint 到新檔案 |
| `r` / `S` | 刷新／明確準備 SDK 環境 |
| `:` / `?` | 可用動作／說明 |
| `m` | 切換滑鼠捕捉，方便終端原生選字 |
| Esc / Ctrl+C | 關閉最近互動／取消等待；完成的寫入不會 rollback |
| `q` | 離開（文字欄位中的 q 仍是文字） |

寬畫面使用來源／項目／詳細資料三欄。80×24 使用兩欄及詳細資料 overlay；窄畫面仍可
用 `s` 換來源、Tab 切焦點、Esc 返回。讀取失敗保留先前結果並標示 stale/error，
重新整理依來源＋項目 ID 保留選取，不以舊 row number 選到別筆資料。

Overview 區分硬體支援、介面啟用、FIPS capability／approved state、鎖定及未知狀態。
FIDO 只列 discoverable credentials，不是歷來所有網站綁定清單。公開憑證存在也不能
證明相符私鑰仍可用；舊裝置查不到 metadata 時保留 unknown。

## 範圍

- YubiKey：OATH、discoverable FIDO credentials、PIV 公開資料、OpenPGP fingerprints／狀態。
- 常用管理：OATH 新增／刪除／密碼、FIDO credential 刪除、應用 PIN 變更。
- Bitwarden：login、card、note、identity、SSH keys 的瀏覽與按需複製；passkey 只呈現 metadata。
- TOTP 複製：指定個人 item UUID 與目標 serial、檢查衝突及 FIPS 條件、寫入後核對；永遠保留來源。
- 不包含：新 vault／同步服務、Bitwarden CRUD、PIV／OpenPGP 私鑰配置、reset、age 初始化、AutoFill。

YubiKey 的 seed、passkey 私鑰與不可匯出的私鑰不提供複製；Bitwarden 可匯出的秘密欄位
與硬體的不可匯出模型不同。HOTP 不在背景產碼；TUI 不隱含推進 counter，CLI 需明確 consent。

Apple Passwords／Chrome／Firefox 第一版先使用官方匯出或 credential exchange 遷移到
Bitwarden，不讀瀏覽器內部資料庫，也不把快照宣稱成即時同步。詳見 [來源與限制](docs/sources.md)。

## Session 與剪貼簿

預設閒置 5 分鐘清除本工具的認證與 vault 畫面；不登出其他 App。秘密揭露 15 秒，
複製 30 秒後只清除仍屬於這次 copy 的內容；新的 copy 不會被舊計時器覆蓋。
清除由有界子程序執行，秘密經 stdin 傳遞、不存檔、不出現在 argv；不依賴 TUI 持續執行。

macOS 使用 pbcopy／pbpaste，Linux 使用 Wayland 的 wl-copy／wl-paste 或 X11 xclip。
沒有可用本機剪貼簿時會回報缺少依賴，不偷偷使用 OSC52。無法清除第三方 clipboard history；
程序遭強制終止、系統關機或外部工具失敗時也不保證清除成功。

設定位於 `$XDG_CONFIG_HOME/lazycred/config.toml`（macOS 同樣預設 `~/.config`），
只有偏好與工具路徑；不存密碼、PIN、session 或 vault。見 [設定](docs/configuration.md)。

## CLI

```sh
lazycred sources --json
lazycred devices list --json
lazycred devices show SERIAL --json
lazycred --source bitwarden items list --json
lazycred --source bitwarden items show ITEM_UUID --json
lazycred --source bitwarden copy ITEM_UUID --field password
lazycred --source yubikey:SERIAL --category oath items list --json
lazycred --source yubikey:SERIAL manage oath-add --name 'Example:alice'
lazycred transfer totp ITEM_UUID --to yubikey:SERIAL --dry-run
lazycred doctor --json
lazycred --read-only
lazycred config show --json
lazycred completion zsh
```

非互動／JSON 不會提示 PIN；CLI vault 操作使用現有 `BW_SESSION`。管理命令要求互動
輸入秘密與確認序號，不提供 `--pin`／`--password`／`--seed` flags。
`--read-only` 禁止管理、setup、transfer 寫入及 HOTP counter；仍可瀏覽、複製與匯出公開資料。
stdout 僅資料或操作結果，診斷在 stderr。JSON 不提供 raw secret 輸出。

## 開發與驗證

```sh
make check
make race
make pty
```

測試使用假 backend、隔離 XDG 路徑與 demo，不讀真實 vault 或寫入硬體。PTY 驗證實際
輸入、paste、resize、modal 與終端還原。CI 在 macOS／Linux 執行相同檢查。
若出現 Go compiler version mismatch，先檢查 `go env GOROOT` 與目前 `go version` 是否
來自同一套安裝；不要為建置本專案改動全域設定，可在確認後單次排除舊的 GOROOT。

[Changelog](CHANGELOG.md) · [MIT](LICENSE) · [Security and data handling](SECURITY.md)
