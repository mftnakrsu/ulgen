---
title: Full guide in Turkish
description: The long guide to Ülgen is written in Turkish. What it covers, and which English pages cover the same ground.
---

The long guide, [EVREN'i kodlama araçlarınızda kullanma](/tr/evren-ile-kodlama-araclari/), is
written in Turkish and has no English version. It goes through every step and its options:

- what you need, with the npm lines that install the four tools;
- signing in, where the key is kept, `ulgen status` and `ulgen logout`;
- listing and setting up the tools, the options of `ulgen setup`, what is written into each tool
  and where the backups go;
- starting a tool with `ulgen run`, or under its own name with the key from `ulgen token`;
- Claude Code through the translator, `ulgen proxy`, and EVREN workers with every option of
  `ulgen worker`;
- what Ülgen does not touch, the project check in `ulgen run`, and tools installed as `.cmd` files
  on Windows;
- undoing the setup, `ulgen doctor`, and a table of the messages you are most likely to see, with
  what to do about each;
- the quota and the models.

In English, the same ground is covered more briefly by [Install](/install/),
[Using Ülgen](/using/) and [EVREN](/evren/). `ulgen <command> -h` lists a command's options, and
the reasoning behind the decisions is in
[docs/design.md](https://github.com/mftnakrsu/ulgen/blob/main/docs/design.md).
