import * as React from "react";
import type { VariantProps } from "class-variance-authority";
import { cn } from "@hdms/ui";
import { buttonVariants } from "./button-variants";

export function Button({
  className,
  variant,
  size,
  type = "button",
  ...props
}: React.ComponentProps<"button"> & VariantProps<typeof buttonVariants>) {
  return <button type={type} className={cn(buttonVariants({ variant, size, className }))} {...props} />;
}
