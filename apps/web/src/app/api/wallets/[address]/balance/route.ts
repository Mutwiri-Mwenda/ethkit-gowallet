
import { NextRequest, NextResponse } from "next/server";

const ADDRESS_PATTERN = /^0x[a-fA-F0-9]{40}$/;

export async function GET(
  _request: NextRequest,
  context: { params: Promise<{ address: string }> }
) {
  const { address } = await context.params;

  if (!ADDRESS_PATTERN.test(address)) {
    return NextResponse.json(
      { error: { code: "invalid_address" } },
      { status: 400 }
    );
  }

  const baseUrl = process.env.GO_API_URL;

  if (!baseUrl) {
    return NextResponse.json(
      { error: { code: "backend_not_configured" } },
      { status: 503 }
    );
  }

  try {
    const response = await fetch(
      `${baseUrl.replace(/\/$/, "")}/api/v1/wallets/${address}/balance`,
      {
        cache: "no-store",
        signal: AbortSignal.timeout(9000),
      }
    );

    const body = await response.text();

    if (!response.ok) {
      const headers = new Headers({
        "Cache-Control": "no-store",
      });

      const retryAfter = response.headers.get("Retry-After");

      if (retryAfter && /^\d+$/.test(retryAfter)) {
        headers.set("Retry-After", retryAfter);
      }

      return NextResponse.json(
        {
          error: {
            code: "balance_unavailable",
          },
        },
        { status: response.status, headers }
      );
    }

    const data: unknown = JSON.parse(body);

    if (
      typeof data !== "object" ||
      data === null ||
      !("balance_wei" in data) ||
      typeof data.balance_wei !== "string" ||
      !/^\d+$/.test(data.balance_wei) ||
      !("chain_id" in data) ||
      data.chain_id !== 11155111 ||
      !("address" in data) ||
      data.address !== address
    ) {
      throw new Error("Invalid upstream response");
    }

    return NextResponse.json(data, {
      headers: { "Cache-Control": "no-store" },
    });
  } catch {
    return NextResponse.json(
      { error: { code: "upstream_unavailable" } },
      {
        status: 502,
        headers: { "Cache-Control": "no-store" },
      }
    );
  }
}
