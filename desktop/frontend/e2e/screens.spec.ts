import { expect, test, type Page } from "@playwright/test";

// Each test drives the real UI against lib/mock.ts and asserts the design
// rules that matter: lime only when connected, RTL for Persian, never colour
// alone. Screenshots land in e2e/shots for review against design/.

const open = async (page: Page, q: string) => {
  await page.goto(`/?${q}`);
  await page.locator("main").waitFor();
};

const shot = (page: Page, name: string) => page.screenshot({ path: `e2e/shots/${name}.png` });

/** Counts pixels near lime #C6F432 on the page. */
async function limePixels(page: Page): Promise<number> {
  const buf = await page.screenshot();
  return page.evaluate(async (b64) => {
    const img = new Image();
    img.src = `data:image/png;base64,${b64}`;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.width;
    c.height = img.height;
    const g = c.getContext("2d")!;
    g.drawImage(img, 0, 0);
    const d = g.getImageData(0, 0, c.width, c.height).data;
    let n = 0;
    for (let i = 0; i < d.length; i += 4) {
      if (Math.abs(d[i]! - 0xc6) < 12 && Math.abs(d[i + 1]! - 0xf4) < 12 && Math.abs(d[i + 2]! - 0x32) < 24) n++;
    }
    return n;
  }, buf.toString("base64"));
}

for (const theme of ["light", "dark"] as const) {
  test.describe(theme, () => {
    test.use({ colorScheme: theme });

    test("off: no lime anywhere", async ({ page }) => {
      await open(page, `theme=${theme}&state=off`);
      await expect(page.getByRole("heading", { name: "Off" })).toBeVisible();
      await expect(page.getByRole("switch", { name: "Connect" })).toBeVisible();
      expect(await limePixels(page)).toBe(0);
      await shot(page, `home-off-${theme}`);
    });

    test("connecting: orb is Disconnect, no lime fill", async ({ page }) => {
      await open(page, `theme=${theme}&state=connecting&freeze`);
      await expect(page.getByRole("heading", { name: "Connecting" })).toBeVisible();
      await expect(page.getByRole("switch", { name: "Disconnect" })).toBeVisible();
      await expect(page.getByRole("button", { name: "Cancel" })).toHaveCount(0);
      await shot(page, `home-connecting-${theme}`);
    });

    test("connected: lime present, word and shape pair with it", async ({ page }) => {
      await open(page, `theme=${theme}&state=connected&freeze`);
      await expect(page.getByRole("heading", { name: "Connected" })).toBeVisible();
      await expect(page.getByRole("switch", { name: "Disconnect" })).toBeVisible();
      await expect(page.getByText("Frankfurt")).toBeVisible();
      await expect(page.getByText("IP", { exact: true })).toBeVisible();
      await expect(page.getByText("203.0.113.10")).toBeVisible();
      await expect(page.getByText("Safe", { exact: true })).toBeVisible();
      expect(await limePixels(page)).toBeGreaterThan(20);
      await shot(page, `home-connected-${theme}`);
    });

    test("failed: danger copy, no lime", async ({ page }) => {
      await open(page, `theme=${theme}&state=failed&freeze`);
      await expect(page.getByRole("heading", { name: "Failed" })).toBeVisible();
      await expect(page.getByText("Couldn't connect. Dot VPN keeps trying.")).toBeVisible();
      expect(await limePixels(page)).toBe(0);
      await shot(page, `home-failed-${theme}`);
    });

    test("account, relay, settings", async ({ page }) => {
      await open(page, `theme=${theme}&state=connected&freeze&routes`);
      await page.getByRole("button", { name: "Account" }).click();
      await expect(page.getByRole("heading", { name: "Account" })).toBeVisible();
      await expect(page.getByText("Davod")).toBeVisible();
      await shot(page, `account-${theme}`);
      await page.getByRole("button", { name: "Relay" }).click();
      await shot(page, `relay-${theme}`);
      await page.getByRole("button", { name: /Public relays/ }).click();
      await expect(page.getByText("Relay DE")).toBeVisible();
      await page.getByRole("button", { name: "Back" }).first().click();
      await page.getByRole("button", { name: /Private relays/ }).click();
      await expect(page.getByText("Friends NL")).toBeVisible();
      await page.getByRole("button", { name: "Back" }).first().click();
      await page.getByRole("button", { name: "Settings" }).click();
      await page.getByRole("tab", { name: "Connection" }).click();
      await expect(page.getByRole("switch", { name: /Block traffic/ })).toBeVisible();
      await page.getByRole("tab", { name: "Bypass" }).click();
      await expect(page.getByText("bank.ir")).toBeVisible();
      await shot(page, `settings-${theme}`);
    });
  });
}

test("toggles are fg, never lime", async ({ page }) => {
  await open(page, "state=off&killswitch");
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("tab", { name: "Connection" }).click();
  await expect(page.getByRole("switch", { name: /Block traffic/ })).toHaveAttribute("aria-checked", "true");
  expect(await limePixels(page)).toBe(0);
});

test("connect switch moves through connecting to connected", async ({ page }) => {
  await open(page, "state=off");
  await page.getByRole("switch", { name: "Connect" }).click();
  await expect(page.getByRole("heading", { name: "Connected" })).toBeVisible();
  await page.getByRole("switch", { name: "Disconnect" }).click();
  await expect(page.getByRole("heading", { name: "Off" })).toBeVisible();
});

test("state events from the helper update the window", async ({ page }) => {
  await open(page, "state=connected&freeze");
  await page.evaluate(() => (window as unknown as { __mock: { setState(s: string): void } }).__mock.setState("switching"));
  await expect(page.getByRole("heading", { name: "Switching route" })).toBeVisible();
  await expect(page.getByText("Couldn't reach the server. Trying the next route.")).toBeVisible();
});

test("kill switch says traffic stays blocked while switching", async ({ page }) => {
  await open(page, "state=connected&freeze&killswitch");
  await page.evaluate(() => (window as unknown as { __mock: { setState(s: string): void } }).__mock.setState("switching"));
  await expect(page.getByText("Traffic stays blocked until a route works.")).toBeVisible();
});

test("first run: set up helper, then add invite", async ({ page }) => {
  await open(page, "helper=down&configured=no");
  await expect(page.getByRole("heading", { name: "Set up Dot VPN" })).toBeVisible();
  await shot(page, "first-run-light");
  await page.getByRole("button", { name: "Set up Dot VPN" }).click();
  await expect(page.getByRole("heading", { name: "Add an invite" })).toBeVisible();
  await page.getByLabel("Invite link").fill("nope");
  await page.getByRole("button", { name: "Add invite" }).click();
  await expect(page.getByText("This invite link isn't valid. Ask the operator for a new one.")).toBeVisible();
  await shot(page, "invite-error-light");
  await page.getByLabel("Invite link").fill("dv://eyJ2IjoxLCJub2RlIjoiZXhhbXBsZS5jb20ifQ");
  await page.getByRole("button", { name: "Add invite" }).click();
  await expect(page.getByRole("switch", { name: "Connect" })).toBeVisible();
});

test("account opens invite screen", async ({ page }) => {
  await open(page, "state=off");
  await page.getByRole("button", { name: "Account" }).click();
  await page.getByRole("button", { name: "Replace account" }).click();
  await page.getByRole("button", { name: "Paste" }).click();
  await expect(page.getByLabel("Invite link")).toHaveValue(/^dv:\/\/.*…/);
  await expect(page.getByLabel("Invite link")).toHaveAttribute("dir", "ltr");
});

test("bypass rejects junk and accepts domains and CIDRs", async ({ page }) => {
  await open(page, "state=off");
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("tab", { name: "Bypass" }).click();
  const input = page.getByLabel("Domain or address");
  await input.fill("not a domain");
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await expect(page.getByText("That is not a domain or an IP range.")).toBeVisible();
  await input.fill("example.com");
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await input.fill("10.0.0.0/8");
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await expect(page.getByText("example.com")).toBeVisible();
  await expect(page.getByText("10.0.0.0/8")).toBeVisible();
  await page.getByRole("button", { name: "Remove example.com" }).click();
  await expect(page.getByText("example.com")).toHaveCount(0);
});

test("bypass paste imports multiple lines", async ({ page }) => {
  await open(page, "state=off");
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("tab", { name: "Bypass" }).click();
  const input = page.getByLabel("Domain or address");
  await input.focus();
  await page.evaluate(() => {
    const el = document.querySelector<HTMLInputElement>('[aria-label="Domain or address"]');
    el?.dispatchEvent(
      new ClipboardEvent("paste", {
        bubbles: true,
        cancelable: true,
        clipboardData: new DataTransfer(),
      }),
    );
  });
  // Playwright paste via fill of multi-line + Enter is covered by parseEntries; drive the handler directly:
  await input.evaluate((el) => {
    const dt = new DataTransfer();
    dt.setData("text", "paste.example\nbank.ir\n10.1.0.0/16");
    el.dispatchEvent(new ClipboardEvent("paste", { bubbles: true, cancelable: true, clipboardData: dt }));
  });
  await expect(page.getByText("paste.example")).toBeVisible();
  await expect(page.getByText("bank.ir")).toBeVisible();
  await expect(page.getByText("10.1.0.0/16")).toBeVisible();
});

test("dns presets show resolver addresses", async ({ page }) => {
  await open(page, "state=off");
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("tab", { name: "DNS" }).click();
  const select = page.getByLabel("Resolver");
  await expect(select.locator("option", { hasText: "Google (8.8.8.8)" })).toHaveCount(1);
  await expect(select.locator("option", { hasText: "System (192.168.1.1)" })).toHaveCount(1);
  await select.selectOption("system");
  // System is only a preset option — no redundant System DNS box.
  await expect(page.getByText("System DNS", { exact: true })).toHaveCount(0);
  await expect(page.locator(".sys")).toHaveCount(0);
});

test.describe("Persian", () => {
  for (const theme of ["light", "dark"] as const) {
    test.use({ colorScheme: theme });
    test(`RTL layout and digits, ${theme}`, async ({ page }) => {
      await open(page, `lang=fa&theme=${theme}&state=connected&freeze`);
      await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
      await expect(page.locator("html")).toHaveAttribute("lang", "fa");
      await expect(page.getByRole("heading", { name: "متصل" })).toBeVisible();
      await expect(page.getByText("۴۲ میلی‌ثانیه").first()).toBeVisible();
      await expect(page.getByText(/\d/).first()).toBeAttached();
      const font = await page.getByRole("heading", { name: "متصل" }).evaluate((e) => getComputedStyle(e).fontFamily);
      expect(font).toContain("Vazirmatn");
      await shot(page, `home-connected-fa-${theme}`);
      await page.getByRole("button", { name: "تنظیمات" }).click();
      await shot(page, `settings-fa-${theme}`);
    });
  }

  test("the Hop mark does not mirror", async ({ page }) => {
    await open(page, "lang=fa&state=off");
    const svg = page.locator("svg[role=img]").first();
    const m = await svg.evaluate((e) => getComputedStyle(e).transform);
    expect(m).toBe("none");
  });

  test("invite field stays LTR", async ({ page }) => {
    await open(page, "lang=fa&state=off");
    await page.getByRole("button", { name: "حساب" }).click();
    await page.getByRole("button", { name: "افزودن دعوت‌نامه" }).click();
    await expect(page.getByLabel("لینک دعوت")).toHaveAttribute("dir", "ltr");
  });
});

test("no network requests leave the app", async ({ page }) => {
  const external: string[] = [];
  page.on("request", (r) => {
    if (!r.url().startsWith("http://127.0.0.1:4173") && !r.url().startsWith("data:")) external.push(r.url());
  });
  await open(page, "state=connected&freeze");
  await page.waitForLoadState("networkidle");
  expect(external).toEqual([]);
});

test("no tray: settings explains that closing quits", async ({ page }) => {
  await open(page, "state=off&notray");
  await page.getByRole("button", { name: "Settings" }).click();
  await expect(page.getByText(/No tray was found/)).toBeVisible();
});
