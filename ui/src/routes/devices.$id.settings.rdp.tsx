import { useCallback, useEffect, useState } from "react";

import { useJsonRpc, type JsonRpcResponse } from "@hooks/useJsonRpc";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

interface RDPConfig {
  enabled: boolean;
  port: number;
  width: number;
  height: number;
  tile_size: number;
  max_sessions: number;
  running: boolean;
  sessions: number;
}

export default function SettingsRDPRoute() {
  const { send } = useJsonRpc();

  const [rdpConfig, setRdpConfig] = useState<RDPConfig | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [port, setPort] = useState(3389);
  const [maxSessions, setMaxSessions] = useState(1);

  const loadRDPConfig = useCallback(() => {
    send("getRDPConfig", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const config = resp.result as RDPConfig;
      setRdpConfig(config);
      setEnabled(config.enabled);
      setPort(config.port);
      setMaxSessions(config.max_sessions);
    });
  }, [send]);

  useEffect(() => {
    loadRDPConfig();
  }, [loadRDPConfig]);

  const saveRDPConfig = useCallback(
    (params: { enabled?: boolean; port?: number; max_sessions?: number }) => {
      send("setRDPConfig", { params }, (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            m.rdp_settings_save_failed({ error: String(resp.error.data || "Unknown error") }),
          );
          return;
        }
        notifications.success(m.rdp_settings_saved());
        loadRDPConfig();
      });
    },
    [send, loadRDPConfig],
  );

  const handleEnabledChange = (value: string) => {
    const newEnabled = value === "true";
    setEnabled(newEnabled);
    saveRDPConfig({ enabled: newEnabled });
  };

  const handleMaxSessionsChange = (value: string) => {
    const newMax = parseInt(value, 10);
    setMaxSessions(newMax);
    saveRDPConfig({ max_sessions: newMax });
  };

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.rdp_title()} description={m.rdp_description()} />

      <SettingsItem
        title={m.rdp_server_enabled()}
        description={m.rdp_server_enabled_description()}
      >
        <SelectMenuBasic
          size="SM"
          value={enabled ? "true" : "false"}
          onChange={e => handleEnabledChange(e.target.value)}
          options={[
            { value: "false", label: "Disabled" },
            { value: "true", label: "Enabled" },
          ]}
        />
      </SettingsItem>

      {enabled && (
        <>
          <SettingsItem title={m.rdp_port_title()} description={m.rdp_port_description()}>
            <span className="text-sm text-slate-600 dark:text-slate-300">{port}</span>
          </SettingsItem>

          <SettingsItem
            title={m.rdp_max_sessions_title()}
            description={m.rdp_max_sessions_description()}
          >
            <SelectMenuBasic
              size="SM"
              value={String(maxSessions)}
              onChange={e => handleMaxSessionsChange(e.target.value)}
              options={[
                { value: "1", label: "1" },
                { value: "2", label: "2" },
                { value: "3", label: "3" },
              ]}
            />
          </SettingsItem>

          {rdpConfig && (
            <SettingsItem title={m.rdp_status_title()}>
              <span
                className={`text-sm font-medium ${
                  rdpConfig.running
                    ? "text-green-600 dark:text-green-400"
                    : "text-slate-500 dark:text-slate-400"
                }`}
              >
                {rdpConfig.running ? m.rdp_status_running() : m.rdp_status_stopped()}
                {rdpConfig.running &&
                  rdpConfig.sessions > 0 &&
                  ` · ${m.rdp_status_sessions({ count: String(rdpConfig.sessions) })}`}
              </span>
            </SettingsItem>
          )}
        </>
      )}
    </div>
  );
}
