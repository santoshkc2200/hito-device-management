import { Button } from "./ui/button";
import { useTranslator } from "@/i18n";

export interface FinishButtonProps {
  onFinish?: () => void;
  isFinishing?: boolean;
}

export function FinishButton({ onFinish, isFinishing = false }: FinishButtonProps) {
  const t = useTranslator();
  if (!onFinish) return null;
  return (
    <Button type="button" size="lg" className="min-h-16 w-full text-xl font-bold" onClick={onFinish} disabled={isFinishing} data-testid="finish-session-button">
      {t(isFinishing ? "summary.finishing" : "common.finish")}
    </Button>
  );
}
