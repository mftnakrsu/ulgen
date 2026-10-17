"""The sentences a user reads stay in README.md.

docs/using.md asks for the range marked there. Links in that range are written
for the repository; this rewrites them for the site.
"""

import re
from pathlib import Path

START = "<!-- site:using-start -->"
END = "<!-- site:using-end -->"
INCLUDE = "<!-- site:using -->"

DESIGN = "https://github.com/mftnakrsu/ulgen/blob/main/docs/design.md"


def on_page_markdown(markdown, page, config, files):
    if INCLUDE not in markdown:
        return markdown
    readme = Path(config.config_file_path).resolve().parent / "README.md"
    text = readme.read_text(encoding="utf-8")
    start = text.find(START)
    end = text.find(END)
    if start < 0 or end < 0 or end < start:
        raise RuntimeError("README.md is missing the site:using markers")
    section = text[start + len(START) : end].strip() + "\n"
    section = re.sub(r"\]\((docs/[^)]+)\)", _site_link, section)
    return markdown.replace(INCLUDE, section)


def _site_link(match):
    target = match.group(1)
    if target == "docs/design.md":
        return "](" + DESIGN + ")"
    if target.startswith("docs/"):
        return "](" + target[len("docs/") :] + ")"
    return match.group(0)
