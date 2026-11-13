"use client";

import { FormEvent, useRef, useState } from "react";
import Link from "next/link";
import Image from "next/image";
import NetworkSelect from "@/components/network-select";
import { ArrowRight, ExternalLink, Search } from "lucide-react";

const NETWORKS = [
  {
    chainId: 1,
    name: "Ethereum Mainnet",
    symbol: "ETH",
    logo: "/networks/ethereum.png",
    explorer: "https://etherscan.io",
  },
  {
    chainId: 42161,
    name: "Arbitrum One",
    symbol: "ETH",
    logo: "/networks/arbitrum.png",
    explorer: "https://arbiscan.io",
  },
  {
    chainId: 137,
    name: "Polygon Mainnet",
    symbol: "POL",
    logo: "/networks/polygon.png",
    explorer: "https://polygonscan.com",
  },
  {
    chainId: 999,
    name: "HyperEVM Mainnet",
    symbol: "HYPE",
    logo: "/networks/hyperevm.png",
    explorer: "https://hyperevmscan.io",
  },
] as const;

type ChainId = (typeof NETWORKS)[number]["chainId"];

type Balance = {
  address: string;
  chain_id: number;
  balance_wei: string;
};

function formatBalance(wei: string): string {
  const amount = BigInt(wei);
  const unit = 10n ** 18n;

  const whole = amount / unit;
  const fraction = (amount % unit)
    .toString()
    .padStart(18, "0")
    .slice(0, 8)
    .replace(/0+$/, "");

  return fraction ? `${whole}.${fraction}` : whole.toString();
}

export default function Home() {
  const [address, setAddress] = useState("");
  const [chainId, setChainId] = useState<ChainId>(1);
  const [balance, setBalance] = useState<Balance | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const requestRef = useRef(0);
  const network = NETWORKS.find((item) => item.chainId === chainId)!;

  function clearResult() {
    requestRef.current += 1;
    setBalance(null);
    setError("");
    setLoading(false);
  }

  async function handleSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const wallet = address.trim();

    if (!/^0x[a-fA-F0-9]{40}$/.test(wallet)) {
      clearResult();
      setError("Enter a valid EVM wallet address.");
      return;
    }

    const requestId = ++requestRef.current;
    const requestedChainId = chainId;

    setLoading(true);
    setError("");
    setBalance(null);

    try {
      const response = await fetch(
        `/api/chains/${requestedChainId}/wallets/${wallet}/balance`,
        { cache: "no-store" }
      );

      if (!response.ok) {
        throw new Error(
          response.status === 503
            ? "The provider is temporarily unavailable. Please try again."
            : response.status === 504
              ? "The request timed out. Please try again."
              : "Unable to retrieve this wallet balance."
        );
      }

      const data: unknown = await response.json();

      if (
        typeof data !== "object" ||
        data === null ||
        !("address" in data) ||
        !("chain_id" in data) ||
        !("balance_wei" in data) ||
        typeof data.address !== "string" ||
        data.address.toLowerCase() !== wallet.toLowerCase() ||
        data.chain_id !== requestedChainId ||
        typeof data.balance_wei !== "string" ||
        !/^\d+$/.test(data.balance_wei)
      ) {
        throw new Error("Unexpected balance response.");
      }

      if (requestId === requestRef.current) {
        setBalance(data as Balance);
      }
    } catch (err) {
      if (requestId === requestRef.current) {
        setError(
          err instanceof Error ? err.message : "Something went wrong."
        );
      }
    } finally {
      if (requestId === requestRef.current) {
        setLoading(false);
      }
    }
  }

  return (
    <div className="flex min-h-screen flex-col bg-white text-[#191919]">
      <header className="mx-auto flex w-full max-w-7xl items-center justify-between px-6 py-6 md:px-12">
        <Link href="/" className="text-xl font-semibold tracking-tight">
          ethkit.
        </Link>

        <nav className="flex items-center gap-6 text-sm">
          <a href="#explorer" className="hover:text-neutral-500">
            Explorer
          </a>
          <a
            href="https://github.com/Mutwiri-Mwenda/ethkit-gowallet"
            target="_blank"
            rel="noopener noreferrer"
            className="rounded-lg bg-[#191919] px-4 py-2.5 text-white transition hover:bg-neutral-700"
          >
            GitHub
          </a>
        </nav>
      </header>

      <main
        id="explorer"
        className="mx-auto flex w-full max-w-4xl flex-1 flex-col items-center px-6 pt-24 text-center md:pt-36"
      >
        <h1 className="text-5xl font-semibold leading-[1.08] tracking-[-0.055em] md:text-7xl">
          Explore Ethereum.
          <br />
          Simply.
        </h1>

        <p className="mt-7 max-w-xl text-base leading-7 text-neutral-500 md:text-lg">
          Explore wallet balances across four EVM networks.
          Built with Go and TypeScript.
        </p>

        <div className="mt-12 w-full max-w-xl text-left">
          <NetworkSelect
            networks={NETWORKS}
            selected={network}
            onChange={(selectedNetwork) => {
              clearResult();
              setChainId(selectedNetwork.chainId);
            }}
          />
        </div>

        <form
          onSubmit={handleSearch}
          className="mt-4 flex w-full max-w-xl gap-2"
        >
          <div className="flex min-w-0 flex-1 items-center gap-3 rounded-lg border border-neutral-200 px-4 focus-within:border-neutral-500">
            <Search size={19} className="shrink-0 text-neutral-400" />
            <input
              aria-label="EVM wallet address"
              value={address}
              onChange={(event) => {
                clearResult();
                setAddress(event.target.value);
              }}
              placeholder="Enter wallet address (0x...)"
              className="w-full min-w-0 bg-transparent py-4 text-sm outline-none placeholder:text-neutral-400"
            />
          </div>

          <button
            type="submit"
            disabled={loading}
            className="flex items-center gap-2 rounded-lg bg-[#191919] px-5 text-sm font-medium text-white transition hover:bg-neutral-700 disabled:opacity-50"
          >
            {loading ? "Searching" : "Explore"}
            {!loading && <ArrowRight size={17} />}
          </button>
        </form>

        <div className="mt-5 flex items-center gap-2 text-xs text-neutral-500">
          <Image
            src={network.logo}
            alt=""
            width={18}
            height={18}
            className="h-[18px] w-[18px] object-contain"
          />
          {network.name}
        </div>

        {error && (
          <p role="alert" className="mt-8 text-sm text-red-600">
            {error}
          </p>
        )}

        {balance && (
          <section className="mt-14 w-full max-w-xl rounded-xl border border-neutral-200 p-7 text-left">
            <p className="text-sm text-neutral-500">Wallet balance</p>

            <h2 className="mt-3 break-all text-3xl font-semibold tracking-tight md:text-4xl">
              {formatBalance(balance.balance_wei)} {network.symbol}
            </h2>

            <div className="mt-7 border-t border-neutral-100 pt-5">
              <p className="text-xs text-neutral-500">Wallet address</p>
              <p className="mt-2 break-all font-mono text-xs">
                {balance.address}
              </p>
            </div>

            <div className="mt-5 flex items-center justify-between text-sm">
              <span className="text-neutral-500">Network</span>
              <span>{network.name} · {balance.chain_id}</span>
            </div>

            <a
              href={`${network.explorer}/address/${balance.address}`}
              target="_blank"
              rel="noopener noreferrer"
              className="mt-7 inline-flex items-center gap-2 text-sm font-medium underline underline-offset-4"
            >
              View on explorer
              <ExternalLink size={15} />
            </a>
          </section>
        )}
      </main>

      <footer className="mx-auto mt-20 flex w-full max-w-7xl flex-wrap items-center justify-between gap-3 border-t border-neutral-100 px-6 py-6 text-xs text-neutral-400 md:px-12">
        <span>© 2026 ethkit</span>
        <span>Built with Go &amp; TypeScript</span>
      </footer>
    </div>
  );
}
