import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { cn } from "../lib";
import { splitMentions } from "../quickadd";

type MdNode = { type: string; value?: string; url?: string; children?: MdNode[] };

// remarkMentions turns @Name in plain text into mention links once the markdown is parsed, so code,
// links and URLs (which aren't plain text nodes) are never touched.
function remarkMentions(names: string[]) {
  return () => (tree: MdNode) => {
    const walk = (node: MdNode) => {
      if (!node.children || node.type === "link" || node.type === "linkReference") return;
      node.children = node.children.flatMap((child): MdNode[] => {
        if (child.type !== "text" || !child.value) {
          walk(child);
          return [child];
        }
        const parts = splitMentions(child.value, names);
        if (!parts.some((p) => p.mention)) return [child];
        return parts.map((p) => (p.mention ? { type: "link", url: "mention:", children: [{ type: "text", value: p.text }] } : { type: "text", value: p.text }));
      });
    };
    walk(tree);
  };
}

// Markdown renders task bodies and comments: GitHub-flavoured (lists, tables, task lists, links),
// with @mentions of members shown as chips. Raw HTML is not rendered.
export function Markdown({ text, mentions = [], className }: { text: string; mentions?: string[]; className?: string }) {
  return (
    <div className={cn("md", className)}>
      <ReactMarkdown
        remarkPlugins={mentions.length ? [remarkGfm, remarkMentions(mentions)] : [remarkGfm]}
        urlTransform={(url) => (url === "mention:" ? url : defaultUrlTransform(url))}
        components={{
          a: ({ href, children }) =>
            href === "mention:" ? (
              <span className="rounded bg-accent px-0.5 font-medium text-foreground">{children}</span>
            ) : (
              <a href={href} target="_blank" rel="noreferrer noopener" onClick={(e) => e.stopPropagation()}>{children}</a>
            ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}
