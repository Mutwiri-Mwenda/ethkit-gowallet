
"use client";

import Image from "next/image";
import {
  Check,
  ChevronDown,
} from "lucide-react";
import {
  useEffect,
  useRef,
  useState,
} from "react";

type Network = {
  chainId: number;
  name: string;
  symbol: string;
  logo: string;
};

type Props<T extends Network> = {
  networks: readonly T[];
  selected: T;
  onChange: (network: T) => void;
};

export default function NetworkSelect<T extends Network>({
  networks,
  selected,
  onChange,
}: Props<T>) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const optionRefs = useRef<(HTMLButtonElement | null)[]>([]);

  function focusOption(index: number) {
    optionRefs.current[index]?.focus();
  }

  useEffect(() => {
    if (!open) return;

    function handlePointerDown(event: PointerEvent) {
      if (
        containerRef.current &&
        !containerRef.current.contains(event.target as Node)
      ) {
        setOpen(false);
      }
    }

    document.addEventListener("pointerdown", handlePointerDown);

    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
    };
  }, [open]);

  function handleTriggerKeyDown(
    event: React.KeyboardEvent<HTMLButtonElement>
  ) {
    if (
      event.key === "ArrowDown" ||
      event.key === "ArrowUp"
    ) {
      event.preventDefault();
      setOpen(true);

      const index = networks.findIndex(
        (network) => network.chainId === selected.chainId
      );

      requestAnimationFrame(() => focusOption(Math.max(0, index)));
    }
  }

  function handleOptionKeyDown(
    event: React.KeyboardEvent<HTMLButtonElement>,
    index: number
  ) {
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
      buttonRef.current?.focus();
    }

    if (event.key === "ArrowDown") {
      event.preventDefault();
      focusOption((index + 1) % networks.length);
    }

    if (event.key === "ArrowUp") {
      event.preventDefault();
      focusOption((index - 1 + networks.length) % networks.length);
    }

    if (event.key === "Home") {
      event.preventDefault();
      focusOption(0);
    }

    if (event.key === "End") {
      event.preventDefault();
      focusOption(networks.length - 1);
    }
  }

  return (
    <div ref={containerRef} className="relative w-full">
      <span
        id="network-label"
        className="mb-2 block text-xs font-medium text-neutral-500"
      >
        Network
      </span>

      <button
        ref={buttonRef}
        type="button"
        aria-labelledby="network-label"
        aria-expanded={open}
        aria-haspopup="true"
        onClick={() => setOpen((value) => !value)}
        onKeyDown={handleTriggerKeyDown}
        className="flex w-full items-center justify-between rounded-xl border border-neutral-200 bg-white px-4 py-3.5 text-left transition hover:border-neutral-300 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-neutral-700"
      >
        <div className="flex items-center gap-3">
          <Image
            src={selected.logo}
            alt=""
            width={34}
            height={34}
            className="h-[34px] w-[34px] object-contain"
          />

          <div>
            <p className="text-sm font-medium text-neutral-900">
              {selected.name}
            </p>
            <p className="mt-0.5 text-xs text-neutral-400">
              {selected.symbol} · Chain {selected.chainId}
            </p>
          </div>
        </div>

        <ChevronDown
          size={18}
          className={`text-neutral-400 transition-transform ${
            open ? "rotate-180" : ""
          }`}
        />
      </button>

      {open && (
        <div
          className="absolute left-0 right-0 top-full z-30 mt-2 overflow-hidden rounded-xl border border-neutral-200 bg-white p-1.5 shadow-[0_12px_40px_rgba(0,0,0,0.09)]"
        >
          {networks.map((network, index) => {
            const active = network.chainId === selected.chainId;

            return (
              <button
                key={network.chainId}
                ref={(element) => {
                  optionRefs.current[index] = element;
                }}
                type="button"
                aria-current={active ? "true" : undefined}
                onClick={() => {
                  onChange(network);
                  setOpen(false);
                  buttonRef.current?.focus();
                }}
                onKeyDown={(event) =>
                  handleOptionKeyDown(event, index)
                }
                className={`flex w-full items-center justify-between rounded-lg px-3 py-3 text-left transition focus-visible:outline-2 focus-visible:outline-neutral-700 ${
                  active
                    ? "bg-neutral-100"
                    : "hover:bg-neutral-50"
                }`}
              >
                <div className="flex items-center gap-3">
                  <Image
                    src={network.logo}
                    alt=""
                    width={32}
                    height={32}
                    className="h-8 w-8 object-contain"
                  />

                  <div>
                    <p className="text-sm font-medium text-neutral-900">
                      {network.name}
                    </p>
                    <p className="mt-0.5 text-xs text-neutral-400">
                      {network.symbol} · Chain {network.chainId}
                    </p>
                  </div>
                </div>

                {active && (
                  <Check size={17} className="text-neutral-800" />
                )}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
