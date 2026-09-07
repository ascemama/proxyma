export function ApplicationFooter({ version }: { version: string }) {
  return (
    <footer className="application-footer">
      <div>
        <span>AGPL-3.0</span>
        <span aria-hidden="true">&middot;</span>
        <a href="https://github.com/ascemama/proxyma" rel="noreferrer" target="_blank">
          Source
        </a>
      </div>
      <span className="application-version">Version {version}</span>
    </footer>
  );
}
