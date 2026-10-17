import prompt from '#lib/agent-prompt.md?raw';
export const prerender = true;
export function GET() {
  return new Response(prompt, { headers: { 'content-type': 'text/markdown; charset=utf-8' } });
}
