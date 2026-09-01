import { Component, type ErrorInfo, type ReactNode } from "react";
import { useT } from "@/i18n";
import { ErrorState } from "./error-state";

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

function RouteErrorFallback({
  error,
  onRetry,
}: {
  error: Error | null;
  onRetry: () => void;
}) {
  const t = useT();
  return (
    <div className="p-6">
      <ErrorState
        title={t("states.pageFailedToRender")}
        detail={error?.message || t("states.unexpectedRenderingError")}
        onRetry={onRetry}
      />
    </div>
  );
}

export class RouteErrorBoundary extends Component<Props, State> {
  public override state: State = {
    hasError: false,
    error: null,
  };

  public static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  public override componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error("RouteErrorBoundary caught an error:", error, errorInfo);
  }

  public override render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }

      return (
        <RouteErrorFallback
          error={this.state.error}
          onRetry={() => {
            this.setState({ hasError: false, error: null });
            window.location.reload();
          }}
        />
      );
    }

    return this.props.children;
  }
}
