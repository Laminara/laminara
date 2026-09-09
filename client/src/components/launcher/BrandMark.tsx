import { useState } from "react";

import { brand, branding } from "@/config/branding";

export function BrandMark() {
  const [broken, setBroken] = useState(false);
  return (
    <div className="flex items-center gap-3">
      {brand().logoDataUri && !broken ? (
        <img src={brand().logoDataUri} alt="" onError={() => setBroken(true)} className="h-9 w-9 rounded-md object-contain" />
      ) : (
        <div className="flex h-9 w-9 items-center justify-center rounded-md bg-primary text-primary-ink">
          <svg viewBox="0 0 32 32" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2.6" strokeLinecap="round">
            <path d="M7 11.5 C12 9.5, 20 9.5, 25 11.5" />
            <path d="M7 16 C12 18, 20 14, 25 16" />
            <path d="M7 20.5 C12 22.5, 20 22.5, 25 20.5" />
          </svg>
        </div>
      )}
      <span className="min-w-0 truncate text-lg font-extrabold tracking-[0.14em]">{branding.name}</span>
    </div>
  );
}
