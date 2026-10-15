import { Extension, type Editor, type Extensions } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import Text from "@tiptap/extension-text";
import HardBreak from "@tiptap/extension-hard-break";
import BulletList from "@tiptap/extension-bullet-list";
import Image from "@tiptap/extension-image";
import Link from "@tiptap/extension-link";
import Placeholder from "@tiptap/extension-placeholder";
import TaskList from "@tiptap/extension-task-list";
import TaskItem from "@tiptap/extension-task-item";
import Table from "@tiptap/extension-table";
import TableRow from "@tiptap/extension-table-row";
import TableCell from "@tiptap/extension-table-cell";
import TableHeader from "@tiptap/extension-table-header";
import { Plugin } from "@tiptap/pm/state";
import { Markdown } from "tiptap-markdown";

// The task body is stored as plain markdown (agents read and write it over the API); the editor is a
// view on it. Everything here is about keeping load -> edit -> save lossless for what agents write:
// GFM tables, task lists, headings, code, quotes and links.

type PMNode = { type: { name: string }; text?: string };
type Parent = { childCount: number; child: (i: number) => PMNode };
type State = {
  text: (s: string) => void;
  write: (s: string) => void;
  esc: (s: string) => string;
  renderList: (node: PMNode, indent: string, marker: () => string) => void;
  inTable?: boolean;
};

// tiptap-markdown turns < and > in text into &lt; &gt; (it assumes HTML rendering). Bodies never
// render raw HTML, so keep them as typed: "a < b" stays readable for agents. In a table cell a bare
// | would start a new column.
const PlainText = Text.extend({
  addStorage: () => ({
    markdown: {
      serialize: (state: State, node: PMNode) => (state.inTable ? state.write(state.esc(node.text ?? "").replace(/\|/g, "\\|")) : state.text(node.text ?? "")),
    },
  }),
});

// Two bullet lists in a row (say, notes then a checklist) would merge into one when read back, so
// alternate the marker between neighbours: - then * then - again.
const bullets = (n: PMNode) => n.type.name === "bulletList" || n.type.name === "taskList";
function serializeBullets(state: State, node: PMNode, parent: Parent, index: number) {
  let n = 0;
  for (let i = index - 1; i >= 0 && bullets(parent.child(i)); i--) n++;
  state.renderList(node, "  ", () => (n % 2 ? "* " : "- "));
}
const Bullets = BulletList.extend({ addStorage: () => ({ markdown: { serialize: serializeBullets } }) });
const Tasks = TaskList.extend({ addStorage: () => ({ markdown: { serialize: serializeBullets } }) });

// A line break inside a table cell would end the row in GFM; write a space there instead.
const Break = HardBreak.extend({
  addStorage: () => ({
    markdown: {
      serialize(state: State, _node: unknown, parent: { childCount: number; child: (i: number) => unknown }, index: number) {
        if (state.inTable) return state.write(" ");
        for (let i = index + 1; i < parent.childCount; i++) if ((parent.child(i) as { type: { name: string } }).type.name !== "hardBreak") return state.write("\\\n");
      },
    },
  }),
});

// GFM cells hold one line, so cells hold one paragraph (Enter in a cell can't split it). Anything
// richer would fall back to an HTML table, which bodies don't render.
const Cell = TableCell.extend({ content: "paragraph" });
const HeaderCell = TableHeader.extend({ content: "paragraph" });

// Checklists: they go loose (blank line between items) without a "tight" attribute, which
// tiptap-markdown only adds to bullet and ordered lists. And a list that mixes "- [ ] task" and
// "- plain" items is split into a checklist and a bullet list, which the editor can hold.
const Checklists = Extension.create({
  name: "checklists",
  addGlobalAttributes: () => [{ types: ["taskList"], attributes: { tight: { default: true, rendered: false } } }],
  addStorage: () => ({
    markdown: {
      parse: {
        updateDOM(root: HTMLElement) {
          root.querySelectorAll("ul.contains-task-list").forEach((ul) => {
            const runs: HTMLElement[][] = [];
            for (const li of [...ul.children] as HTMLElement[]) {
              const last = runs.at(-1);
              if (last && last[0].classList.contains("task-list-item") === li.classList.contains("task-list-item")) last.push(li);
              else runs.push([li]);
            }
            if (runs.length < 2) return;
            for (const run of runs) {
              const list = document.createElement("ul");
              list.setAttribute("data-tight", "true");
              if (run[0].classList.contains("task-list-item")) { list.className = "contains-task-list"; list.setAttribute("data-type", "taskList"); }
              list.append(...run);
              ul.before(list);
            }
            ul.remove();
          });
        },
      },
    },
  }),
});

// Keep a paragraph after a closing table, code block, list or quote so there's always somewhere to
// click and keep writing. An empty one serializes to nothing.
const TrailingParagraph = Extension.create({
  name: "trailingParagraph",
  addProseMirrorPlugins: () => [
    new Plugin({
      appendTransaction: (_trs, _old, state) =>
        state.doc.lastChild?.type.name === "paragraph" ? null : state.tr.insert(state.doc.content.size, state.schema.nodes.paragraph.create()),
    }),
  ],
});

// Pictures from elsewhere show as links while editing too: loading them would tell their host who
// read the text. The markdown keeps the image as written.
const OwnImages = Image.extend({
  renderHTML({ node, HTMLAttributes }) {
    const src = String(node.attrs.src ?? "");
    if (/^\/(?!\/)/.test(src)) return ["img", HTMLAttributes];
    return ["a", { href: src, target: "_blank", rel: "noreferrer noopener" }, String(node.attrs.alt || "Image")];
  },
});

export function richExtensions(placeholder = ""): Extensions {
  return [
    StarterKit.configure({ text: false, hardBreak: false, bulletList: false }),
    PlainText,
    Break,
    Bullets,
    Link.configure({ openOnClick: false, autolink: true, linkOnPaste: true, HTMLAttributes: { rel: "noreferrer noopener", target: "_blank" } }),
    OwnImages.configure({ inline: true }),
    Placeholder.configure({ placeholder }),
    Tasks,
    TaskItem.configure({ nested: true }),
    Checklists,
    Table.configure({ resizable: false }),
    TableRow,
    HeaderCell,
    Cell,
    TrailingParagraph,
    Markdown.configure({ html: false, tightLists: true, bulletListMarker: "-", linkify: false, breaks: false, transformPastedText: true, transformCopiedText: true }),
  ];
}

// toMarkdown reads the editor back as the markdown we store. Runs of blank lines between blocks
// collapse to one; inside a code fence they're content and stay as they are.
export function toMarkdown(editor: Editor): string {
  const md = (editor.storage as { markdown: { getMarkdown: () => string } }).markdown.getMarkdown();
  const out: string[] = [];
  let fence = "";
  for (const line of md.split("\n")) {
    const m = /^\s*(`{3,}|~{3,})(.*)$/.exec(line);
    if (fence) {
      if (m && m[1][0] === fence[0] && m[1].length >= fence.length && !m[2].trim()) fence = "";
    } else if (m) fence = m[1];
    else if (line === "" && out.at(-1) === "" && out.at(-2) === "") continue;
    out.push(line);
  }
  return out.join("\n").trim();
}
