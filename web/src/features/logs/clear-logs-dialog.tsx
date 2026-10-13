import { useState } from "react";
import { Eraser, MoreHorizontal } from "lucide-react";
import { useClearLogs, useMe, useProjectUsage } from "@/api/hooks";
import { Button, Dialog, Field, IconButton, Input, MenuItem, Popover, toast } from "@/components/ui";
import { fmtNumber } from "@/lib/time";

/** Delete every message of a project, after the project id is typed in. */
export function ClearLogsDialog({ project, onClose }: { project: { id: string; name: string }; onClose: () => void }) {
  const [typed, setTyped] = useState("");
  const clear = useClearLogs();
  const usage = useProjectUsage(project.id);
  const rows = usage.data?.by_source.logs?.rows;
  return (
    <Dialog open onClose={onClose} title={`Clear all messages in ${project.name}?`}>
      <p className="text-[13px] text-ink-2">
        Every message on the Logs page of this project is deleted{rows ? `, about ${fmtNumber(rows)} of them` : ""}, with the message counts and request statistics built from them. Issues, request timelines and custom measurements stay. This can't be undone.
      </p>
      <Field label={`Type ${project.id} to confirm`} className="mt-3">
        <Input value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus autoComplete="off" />
      </Field>
      {clear.isError && (
        <div role="alert" className="mt-2 text-[13px] text-critical-text">
          {String((clear.error as Error).message ?? clear.error)}
        </div>
      )}
      <div className="mt-4 flex justify-end gap-2">
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="danger"
          disabled={typed.trim() !== project.id}
          loading={clear.isPending}
          onClick={() =>
            clear.mutate(project.id, {
              onSuccess: () => {
                toast.success("Messages cleared", "The disk space is freed in the background.");
                onClose();
              },
            })
          }
        >
          Clear all messages
        </Button>
      </div>
    </Dialog>
  );
}

/** The Logs page's "More" menu: actions on all of the project's messages, for admins. */
export function LogsMoreMenu() {
  const me = useMe();
  const [confirm, setConfirm] = useState(false);
  const project = me.data?.project;
  if (!me.data?.is_admin || !project) return null;
  return (
    <>
      <Popover align="end" width={220} trigger={({ toggle, open }) => <IconButton label="More" icon={<MoreHorizontal size={16} />} active={open} onClick={toggle} variant="default" />}>
        {(close) => (
          <MenuItem danger icon={<Eraser size={14} />} onClick={() => (close(), setConfirm(true))}>
            Clear all messages…
          </MenuItem>
        )}
      </Popover>
      {confirm && <ClearLogsDialog project={project} onClose={() => setConfirm(false)} />}
    </>
  );
}
