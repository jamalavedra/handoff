import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { useRouter } from "@tanstack/react-router";
import { cn } from "../lib";
import { splitMentions } from "../quickadd";
import { target } from "../tabs";
import { TaskLink } from "./tasklink";

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
            ) : href && /^\/(?!\/)/.test(href) ? (
              <AppLink href={href}>{children}</AppLink>
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

// AppLink keeps links to the app's own pages in the app: a task opens the way any task link
// does, other paths go through the router. Modified clicks are left to the browser.
function AppLink({ href, children }: { href: string; children: React.ReactNode }) {
  const router = useRouter();
  const t = target(href);
  const task = t.kind === "task" && /^\/tasks\/[^/?#]+\/?$/.test(href) ? t.id : undefined;
  if (task) {
    return (
      <span className="contents" onClick={(e) => e.stopPropagation()}>
        <TaskLink id={task}>{children}</TaskLink>
      </span>
    );
  }
  return (
    <a
      href={href}
      onClick={(e) => {
        e.stopPropagation();
        if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        router.history.push(href);
      }}
    >
      {children}
    </a>
  );
}
