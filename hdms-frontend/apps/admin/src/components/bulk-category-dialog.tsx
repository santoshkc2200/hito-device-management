import { type Category, updateDevice } from "@hdms/api-client";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface BulkCategoryDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  selectedDevices: Array<{ id: string; name: string; assetTag: string }>;
  categories: Category[];
  onDone: () => void;
}

export function BulkCategoryDialog({
  open,
  onOpenChange,
  selectedDevices,
  categories,
  onDone,
}: BulkCategoryDialogProps) {
  const queryClient = useQueryClient();
  const [selectedCategoryId, setSelectedCategoryId] = useState("");

  const targetCategory = categories.find((c) => c.id === selectedCategoryId);

  const mutation = useMutation({
    mutationFn: async () => {
      if (!selectedCategoryId) return;
      const promises = selectedDevices.map((d) =>
        updateDevice({
          path: { id: d.id },
          body: {
            name: d.name,
            categoryId: selectedCategoryId,
          },
        }),
      );
      const results = await Promise.all(promises);
      const errors = results.filter((r) => r.error);
      if (errors.length > 0) {
        throw new Error(`Failed to update ${errors.length} devices`);
      }
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["devices"] });
      toast.success(
        `Updated category to ${targetCategory?.name ?? "selected"} for ${selectedDevices.length} devices`,
      );
      setSelectedCategoryId("");
      onDone();
    },
    onError: (err: any) => {
      toast.error(err?.message || "Could not update categories");
    },
  });

  const handleClose = () => {
    setSelectedCategoryId("");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={(o) => (!o ? handleClose() : onOpenChange(true))}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Change category for {selectedDevices.length} devices</DialogTitle>
          <DialogDescription>
            This action will change the category for all {selectedDevices.length} selected
            devices to the category chosen below.
          </DialogDescription>
        </DialogHeader>

        <div className="py-3">
          <label className="text-xs font-semibold text-muted-foreground uppercase">
            New Category
          </label>
          <div className="mt-1.5">
            <Select value={selectedCategoryId} onValueChange={setSelectedCategoryId}>
              <SelectTrigger className="w-full">
                <SelectValue placeholder="Select target category…" />
              </SelectTrigger>
              <SelectContent>
                {categories.map((cat) => (
                  <SelectItem key={cat.id} value={cat.id}>
                    {cat.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <DialogFooter className="gap-2 sm:gap-0">
          <Button variant="outline" onClick={handleClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button
            disabled={!selectedCategoryId || mutation.isPending}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending
              ? "Updating…"
              : `Update ${selectedDevices.length} ${selectedDevices.length === 1 ? "device" : "devices"}`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
