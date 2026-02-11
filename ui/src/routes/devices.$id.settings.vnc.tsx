import { useCallback, useEffect, useState } from "react";
import { LuMonitorSmartphone } from "react-icons/lu";

import { useJsonRpc, type JsonRpcResponse } from "@hooks/useJsonRpc";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SettingsSectionHeader } from "@components/SettingsSectionHeader";
import { SettingsItem } from "@components/SettingsItem";
import InputField, { InputFieldWithLabel } from "@components/InputField";
import { Button } from "@components/Button";
import { Checkbox } from "@components/Checkbox";
import notifications from "@/notifications";

interface VNCState {
  enabled: boolean;
  port: number;
  hasPassword: boolean;
  connectedClients: number;
}

export default function SettingsVNCRoute() {
  const { send } = useJsonRpc();

  const [vncState, setVncState] = useState<VNCState | null>(null);
  const [port, setPort] = useState("5900");
  const [password, setPassword] = useState("");
  const [showPasswordField, setShowPasswordField] = useState(false);
  const [isSaving, setIsSaving] = useState(false);

  const fetchVNCState = useCallback(() => {
    send("getVNCState", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return console.error(resp.error);
      const state = resp.result as VNCState;
      setVncState(state);
      setPort(String(state.port));
    });
  }, [send]);

  useEffect(() => {
    fetchVNCState();
  }, [fetchVNCState]);

  const handleToggleEnabled = useCallback(
    (enabled: boolean) => {
      send("setVNCEnabled", { enabled }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(`Failed to ${enabled ? "enable" : "disable"} VNC: ${resp.error.message}`);
          return;
        }
        notifications.success(`VNC server ${enabled ? "enabled" : "disabled"}`);
        fetchVNCState();
      });
    },
    [send, fetchVNCState],
  );

  const handleSavePort = useCallback(() => {
    const portNum = parseInt(port, 10);
    if (isNaN(portNum) || portNum < 1 || portNum > 65535) {
      notifications.error("Port must be between 1 and 65535");
      return;
    }
    setIsSaving(true);
    send("setVNCPort", { port: portNum }, (resp: JsonRpcResponse) => {
      setIsSaving(false);
      if ("error" in resp) {
        notifications.error(`Failed to set VNC port: ${resp.error.message}`);
        return;
      }
      notifications.success("VNC port updated");
      fetchVNCState();
    });
  }, [send, port, fetchVNCState]);

  const handleSavePassword = useCallback(() => {
    setIsSaving(true);
    send("setVNCPassword", { password }, (resp: JsonRpcResponse) => {
      setIsSaving(false);
      if ("error" in resp) {
        notifications.error(`Failed to set VNC password: ${resp.error.message}`);
        return;
      }
      notifications.success(password ? "VNC password set" : "VNC password removed");
      setPassword("");
      setShowPasswordField(false);
      fetchVNCState();
    });
  }, [send, password, fetchVNCState]);

  const handleRemovePassword = useCallback(() => {
    setIsSaving(true);
    send("setVNCPassword", { password: "" }, (resp: JsonRpcResponse) => {
      setIsSaving(false);
      if ("error" in resp) {
        notifications.error(`Failed to remove VNC password: ${resp.error.message}`);
        return;
      }
      notifications.success("VNC password removed");
      fetchVNCState();
    });
  }, [send, fetchVNCState]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader
        title="VNC"
        description="Configure VNC remote access to control the target machine using any standard VNC client."
      />

      <div className="flex items-center gap-x-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
        <LuMonitorSmartphone className="h-4 w-4 shrink-0" />
        <span>
          <strong>Experimental:</strong> VNC support is experimental. Performance may vary and
          some features may be limited.
        </span>
      </div>

      <SettingsSectionHeader
        title="Server"
        description="Enable or disable the VNC server and configure its network settings."
      />

      <SettingsItem
        title="Enable VNC Server"
        description="Start a VNC server that allows remote control via standard VNC clients."
        badge="Experimental"
      >
        <Checkbox
          checked={vncState?.enabled ?? false}
          onChange={e => handleToggleEnabled(e.target.checked)}
        />
      </SettingsItem>

      <SettingsItem
        title="Port"
        description={`TCP port the VNC server listens on.${vncState?.enabled ? ` Currently ${vncState.connectedClients} client(s) connected.` : ""}`}
      >
        <div className="flex items-center gap-x-2">
          <InputField
            size="SM"
            type="number"
            min={1}
            max={65535}
            value={port}
            onChange={e => setPort(e.target.value)}
            disabled={!vncState?.enabled}
          />
          <Button
            size="SM"
            theme="primary"
            text="Save"
            onClick={handleSavePort}
            disabled={!vncState?.enabled || isSaving || port === String(vncState?.port)}
            data-testid="vnc-save-port-button"
          />
        </div>
      </SettingsItem>

      <div className="h-px w-full bg-slate-800/10 dark:bg-slate-300/20" />

      <SettingsSectionHeader
        title="Authentication"
        description="Protect VNC access with a password. When no password is set, any client can connect."
      />

      <SettingsItem
        title="Password Protection"
        description={
          vncState?.hasPassword
            ? "A VNC password is currently set. Clients must authenticate to connect."
            : "No VNC password is set. Anyone on the network can connect."
        }
      >
        {vncState?.hasPassword ? (
          <div className="flex items-center gap-x-2">
            <Button
              size="SM"
              theme="light"
              text="Change"
              onClick={() => setShowPasswordField(true)}
              data-testid="vnc-change-password-button"
            />
            <Button
              size="SM"
              theme="danger"
              text="Remove"
              onClick={handleRemovePassword}
              disabled={isSaving}
              data-testid="vnc-remove-password-button"
            />
          </div>
        ) : (
          <Button
            size="SM"
            theme="primary"
            text="Set Password"
            onClick={() => setShowPasswordField(true)}
            data-testid="vnc-set-password-button"
          />
        )}
      </SettingsItem>

      {showPasswordField && (
        <div className="ml-4 space-y-3 rounded-md border border-slate-200 bg-slate-50 p-4 dark:border-slate-700 dark:bg-slate-900">
          <InputFieldWithLabel
            label="VNC Password"
            description="Password must be 1-8 characters (VNC protocol limitation)."
            type="password"
            size="SM"
            value={password}
            onChange={e => setPassword(e.target.value)}
            maxLength={8}
            placeholder="Enter VNC password"
            autoFocus
          />
          <div className="flex items-center gap-x-2">
            <Button
              size="SM"
              theme="primary"
              text="Save Password"
              onClick={handleSavePassword}
              disabled={isSaving || password.length === 0}
              data-testid="vnc-save-password-button"
            />
            <Button
              size="SM"
              theme="light"
              text="Cancel"
              onClick={() => {
                setShowPasswordField(false);
                setPassword("");
              }}
              data-testid="vnc-cancel-password-button"
            />
          </div>
        </div>
      )}
    </div>
  );
}
