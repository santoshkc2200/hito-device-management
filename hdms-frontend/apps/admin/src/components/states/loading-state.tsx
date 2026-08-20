import { Skeleton } from "@/components/ui/skeleton";

interface LoadingStateProps {
  message?: string;
  className?: string;
}

export function LoadingState({ message = "Loading...", className = "" }: LoadingStateProps) {
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
      <span className="sr-only">{message}</span>
    </div>
  );
}
