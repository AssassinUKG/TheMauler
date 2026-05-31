#!/usr/bin/env python3
"""
browser_agent.py — autonomous browser task runner for TheMauler.

Usage:
    python scripts/browser_agent.py "<natural language task>"

Setup (one-time):
    pip install browser-use langchain-openai
    playwright install chromium

Configuration via environment variables:
    BROWSER_USE_API_BASE  — LLM endpoint (default: http://localhost:1234/v1)
    BROWSER_USE_API_KEY   — API key (default: lm-studio)
    BROWSER_USE_MODEL     — model name (default: qwen3.6-27b)
    BROWSER_USE_HEADLESS  — run headless browser (default: true)
    BROWSER_USE_MAX_STEPS — max agent steps (default: 25)
"""

import asyncio
import os
import sys


def check_deps() -> None:
    missing = []
    try:
        import browser_use  # noqa: F401
    except ImportError:
        missing.append("browser-use")
    try:
        import langchain_openai  # noqa: F401
    except ImportError:
        missing.append("langchain-openai")
    if missing:
        print(
            f"Missing dependencies: {', '.join(missing)}\n"
            f"Run: pip install {' '.join(missing)} && playwright install chromium",
            file=sys.stderr,
        )
        sys.exit(1)


async def run_task(task: str) -> str:
    from browser_use import Agent, BrowserConfig, BrowserContextConfig
    from browser_use.browser.browser import Browser
    from langchain_openai import ChatOpenAI

    api_base = os.environ.get("BROWSER_USE_API_BASE", "http://localhost:1234/v1")
    api_key = os.environ.get("BROWSER_USE_API_KEY", "lm-studio")
    model = os.environ.get("BROWSER_USE_MODEL", "qwen3.6-27b")
    headless = os.environ.get("BROWSER_USE_HEADLESS", "true").lower() != "false"
    max_steps = int(os.environ.get("BROWSER_USE_MAX_STEPS", "25"))

    llm = ChatOpenAI(
        base_url=api_base,
        api_key=api_key,
        model=model,
        temperature=0.0,
    )

    browser = Browser(
        config=BrowserConfig(
            headless=headless,
            new_context_config=BrowserContextConfig(no_viewport=False),
        )
    )

    agent = Agent(
        task=task,
        llm=llm,
        browser=browser,
        max_actions_per_step=5,
    )

    history = await agent.run(max_steps=max_steps)
    await browser.close()

    result = history.final_result()
    return result if result else "Task completed with no explicit result."


def main() -> None:
    if len(sys.argv) < 2:
        print("usage: browser_agent.py <task>", file=sys.stderr)
        sys.exit(1)

    task = sys.argv[1].strip()
    if not task:
        print("browser_agent: task must not be empty", file=sys.stderr)
        sys.exit(1)

    check_deps()

    result = asyncio.run(run_task(task))
    print(result)


if __name__ == "__main__":
    main()
