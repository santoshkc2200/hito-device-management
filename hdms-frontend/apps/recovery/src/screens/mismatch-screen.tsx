import { Message } from "@/components/message";
import { Button } from "@/components/ui/button";
import { useT } from "@/i18n";

// The bundle opened, but its secrets are not this server's: restoring would
// bring back badges and TOTP no one could use. Only install.sh --restore
// (plan 4) can give this server the right keys.
export function MismatchScreen({ onBack }: { onBack: () => void }) {
  const t = useT();
  return (
    <section className="space-y-6">
      <h2 className="text-lg font-semibold">{t("mismatch.heading")}</h2>
      <Message tone="error" testId="keys-mismatch">
        <p>{t("mismatch.body")}</p>
        <p className="mt-2 font-semibold">{t("mismatch.action")}</p>
      </Message>
      <Button variant="outline" onClick={onBack}>
        {t("common.back")}
      </Button>
    </section>
  );
}
