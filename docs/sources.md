# 資料來源與限制

查核日期：2026-10-26。

## YubiKey

使用 [YubiKey Manager CLI／Python SDK](https://developers.yubico.com/yubikey-manager/)，
不是自製 FIDO、APDU、PIV 或 OTP 實作。helper 固定依賴版本，資料經 stdin／JSON pipe
往返；官方庫的 raw exception／debug 不回放到畫面或 log。

每把裝置獨立顯示。相同 issuer／username 在不同金鑰上仍是不同項目；寫入重新確認
完整 serial 與 exact ID。FIDO list 只涵蓋 discoverable credentials；PIV certificate
與 private key 是不同物件，metadata 未知時不能當作空 slot。FIPS approved state
不由型號名稱推定。[官方 FIDO 說明](https://docs.yubico.com/software/yubikey/tools/authenticator/auth-guide/fido2.html)

CCID-only 裝置可能啟用 touch-eject：reader 在，但 card 尚未插入，輕碰一次再 refresh。
這不表示需要改 PIN／reset。[touch-eject](https://docs.yubico.com/yesdk/yubikey-api/Yubico.YubiKey.DeviceFlags.html)

## Bitwarden

沿用已登入的 [官方 bw CLI](https://bitwarden.com/help/cli/)，不另做登入、加密、同步服務。
TUI 可解鎖或使用現有 `BW_SESSION`；CLI 一次性操作使用環境中的 session。
Refresh 明確執行 bw sync；啟動只取得本機狀態，顯示 last sync，不宣稱快取一定最新。

多數列表是 metadata，但原生 bw JSON 可以包含秘密；原始回應只留記憶體，不能開
debug dump／記錄 request payload。passkey 的 private key material 不成為可複製欄位。
TOTP transfer 只支援個人 login，拒絕組織資料、HOTP／Steam 自動遷移；來源永遠保留。

帶有 master-password re-prompt 設定的項目仍可瀏覽 metadata，但首版不繞過該設定；
秘密欄位需回官方 Bitwarden App 操作。

## Apple Passwords／瀏覽器

第一版不直接讀取 iCloud Keychain、手機或瀏覽器資料庫。先透過來源的官方功能移到
Bitwarden，再由 bw 使用；這是一次遷移，不是雙向同步。

- 支援的 iOS 26+ App 可用 CXP 將 Apple Passwords 的選定 passwords／passkeys 轉至
  Bitwarden：[官方步驟](https://bitwarden.com/help/import-from-safari/)。
- Chrome 提供官方匯出／匯入：[Google Password Manager](https://support.google.com/chrome/answer/13068232?hl=en)。
- Firefox 可匯出 CSV：[Mozilla 說明](https://support.mozilla.org/en-US/kb/export-login-data-firefox)。

明文匯出檔需依官方建議妥善處理；lazycred 不自動保存一份，也不掃描 Downloads 尋找它。
手機／瀏覽器的 AutoFill 繼續使用原 App 或 extension；TUI 的優勢是終端中的快速搜尋、
明確複製、硬體狀態與跨來源操作。
