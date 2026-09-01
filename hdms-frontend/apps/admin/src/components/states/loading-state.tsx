import { Skeleton } from "@/components/ui/skeleton";
import { useT } from "@/i18n";

interface LoadingStateProps {
  message?: string;
  className?: string;
}

export function LoadingState({ message, className = "" }: LoadingStateProps) {
  const t = useT();
  const displayMessage = message ?? t("states.loading");

  return (
    <div
      role="status"
      aria-live="polite"
      className={`flex flex-col items-center justify-center p-12 text-center ${className}`}
    >
      <div className="w-full max-w-md space-y-3">
        <Skeleton className="h-8 w-3/4 mx-auto" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-5/6 mx-auto" />
        <Skeleton className="h-10 w-32 mx-auto mt-4" />
      </div>
      <span className="sr-only">{displayMessage}</span>
    </div>
  );
}
