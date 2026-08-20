import type { Problem } from "@hdms/api-client";
import { toast } from "sonner";

export const notify = {
  success(message: string, description?: string) {
    toast.success(message, {
      description,
    });
  },

  error(error: Problem | Error | string | unknown, fallbackMessage = "An error occurred") {
    if (typeof error === "string") {
      toast.error(error);
      return;
    }

    if (error && typeof error === "object") {
      const prob = error as Problem & {
        message?: string;
        task?: string;
        extensions?: Record<string, unknown>;
      };

      const task =
        prob.task ||
        (prob.extensions?.task as string | undefined) ||
        (typeof prob.extensions === "object" && prob.extensions !== null && "task" in prob.extensions
          ? String(prob.extensions.task)
          : undefined);

      let title = prob.title || prob.message || fallbackMessage;
      if (task) {
        title = `${title} (Task ${task})`;
      }

      toast.error(title, {
        description: prob.detail,
      });
      return;
    }

    toast.error(fallbackMessage);
  },

  info(message: string, description?: string) {
    toast.info(message, {
      description,
    });
  },

  warning(message: string, description?: string) {
    toast.warning(message, {
      description,
    });
  },
};
