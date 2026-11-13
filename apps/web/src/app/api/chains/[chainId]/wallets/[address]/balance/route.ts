
import { NextRequest, NextResponse } from "next/server";

const SUPPORTED_CHAINS = new Set(["1", "42161", "137", "999"]);

export async function GET(
  request: NextRequest,
  context: {
    params: Promise<{ chainId: string; address: string }>;
  }
) {
  const { chainId, address } = await context.params;

  if (!SUPPORTED_CHAINS.has(chainId)) {
    return NextResponse.json(
      { error: { code: "unsupported_chain" } },
      { status: 400 }
    );
  }

  if (!/^0x[a-fA-F0-9]{40}$/.test(address)) {
    return NextResponse.json(
      { error: { code: "invalid_address" } },
      { status: 400 }
    );
  }

  const baseURL = process.env.GO_API_URL;

  if (!baseURL) {
    return NextResponse.json(
      { error: { code: "backend_not_configured" } },
      { status: 503 }
    );
  }

  try {
    const upstream = await fetch(
      `${baseURL.replace(/\/$/, "")}/api/v1/chains/${chainId}/wallets/${address}/balance`,
      {
        cache: "no-store",
        signal: AbortSignal.timeout(10000),
      }
    );

    const body = await upstream.text();

    return new NextResponse(body, {
      status: upstream.status,
      headers: {
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
      },
    });
  } catch {
    return NextResponse.json(
      { error: { code: "backend_unavailable" } },
      { status: 502 }
    );
  }
}
