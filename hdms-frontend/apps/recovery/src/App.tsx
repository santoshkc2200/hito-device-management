import * as React from "react";
import { LocaleProvider, type Locale } from "@hdms/i18n";
import { Page } from "@/components/page";
import type { Snapshot, Source } from "@/lib/api";
import { ConfirmScreen } from "@/screens/confirm-screen";
import { MismatchScreen } from "@/screens/mismatch-screen";
import { ProgressScreen } from "@/screens/progress-screen";
import { SnapshotsScreen } from "@/screens/snapshots-screen";
import { StartScreen } from "@/screens/start-screen";
import { UnlockScreen } from "@/screens/unlock-screen";

type Stage =
  | { name: "start" }
  | { name: "unlock"; source: Source; sessionEnded: boolean }
  | { name: "mismatch" }
  | { name: "snapshots"; source: Source }
  | { name: "confirm"; source: Source; snapshot: Snapshot }
  | { name: "confirmUndo"; source: Source; snapshotTakenAt: string }
  | { name: "progress"; source: Source };

// One page, no router: the flow is linear and the worker holds the only
// state that matters (the session and the restore).
export function RecoveryFlow({ pollMs }: { pollMs?: number }) {
  const [stage, setStage] = React.useState<Stage>({ name: "start" });

  switch (stage.name) {
    case "start":
      return <StartScreen onChoose={(source) => setStage({ name: "unlock", source, sessionEnded: false })} />;
    case "unlock":
      return (
        <UnlockScreen
          source={stage.source}
          sessionEnded={stage.sessionEnded}
          onUnlocked={() => setStage({ name: "snapshots", source: stage.source })}
          onMismatch={() => setStage({ name: "mismatch" })}
          onBack={() => setStage({ name: "start" })}
        />
      );
    case "mismatch":
      return <MismatchScreen onBack={() => setStage({ name: "start" })} />;
    default: {
      // Every screen behind the key returns here when the worker forgets the
      // session (30 minutes idle, or a worker restart).
      const source = stage.source;
      const sessionLost = () => setStage({ name: "unlock", source, sessionEnded: true });
      const toProgress = () => setStage({ name: "progress", source });
      const toUndo = (snapshotTakenAt: string) => setStage({ name: "confirmUndo", source, snapshotTakenAt });
      const toSnapshots = () => setStage({ name: "snapshots", source });
      switch (stage.name) {
        case "snapshots":
          return (
            <SnapshotsScreen
              onPick={(snapshot) => setStage({ name: "confirm", source, snapshot })}
              onUndo={toUndo}
              onRunning={toProgress}
              onSessionLost={sessionLost}
            />
          );
        case "confirm":
          return (
            <ConfirmScreen
              mode={{ kind: "restore", snapshot: stage.snapshot }}
              onStarted={toProgress}
              onBack={toSnapshots}
              onSessionLost={sessionLost}
            />
          );
        case "confirmUndo":
          return (
            <ConfirmScreen
              mode={{ kind: "undo", snapshotTakenAt: stage.snapshotTakenAt }}
              onStarted={toProgress}
              onBack={toSnapshots}
              onSessionLost={sessionLost}
            />
          );
        case "progress":
          return <ProgressScreen pollMs={pollMs} onUndo={toUndo} onRestart={toSnapshots} onSessionLost={sessionLost} />;
      }
    }
  }
}

export function App({ initialLocale, pollMs }: { initialLocale?: Locale; pollMs?: number }) {
  return (
    <LocaleProvider locale={initialLocale}>
      <Page>
        <RecoveryFlow pollMs={pollMs} />
      </Page>
    </LocaleProvider>
  );
}
