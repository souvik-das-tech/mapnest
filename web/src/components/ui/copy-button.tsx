import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { copyText } from "@/lib/utils";
import { Button } from "./button";

export function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <Button
      size="sm"
      variant="outline"
      onClick={async () => {
        if (await copyText(text)) {
          setDone(true);
          setTimeout(() => setDone(false), 1500);
        }
      }}
    >
      {done ? <Check /> : <Copy />}
      {done ? "Copied" : label}
    </Button>
  );
}
