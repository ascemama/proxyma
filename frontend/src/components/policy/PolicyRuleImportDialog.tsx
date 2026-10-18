import { useState } from 'react';
import { APIError, type PolicyRuleImportResponse, type PolicyRuleListDocument } from '../../api/client';

type PolicyRuleImportDialogProps = {
  error: Error | null;
  isSaving: boolean;
  onClose: () => void;
  onImport: (document: PolicyRuleListDocument) => Promise<PolicyRuleImportResponse>;
};

export function PolicyRuleImportDialog({ error, isSaving, onClose, onImport }: PolicyRuleImportDialogProps) {
  const [document, setDocument] = useState<PolicyRuleListDocument | null>(null);
  const [fileError, setFileError] = useState('');
  const [result, setResult] = useState<PolicyRuleImportResponse | null>(null);

  async function readFile(file: File) {
    setFileError('');
    try {
      const parsed = JSON.parse(await file.text()) as PolicyRuleListDocument;
      if (parsed.version !== 1 || !Array.isArray(parsed.blacklist) || !Array.isArray(parsed.whitelist)) {
        throw new Error('The file must contain version 1, blacklist, and whitelist arrays.');
      }
      setDocument(parsed);
    } catch (caught) {
      setDocument(null);
      setFileError(caught instanceof Error ? caught.message : 'The selected file is not valid JSON.');
    }
  }

  async function submit() {
    if (!document) return;
    try {
      setResult(await onImport(document));
    } catch {
      // Parent mutation error is displayed below.
    }
  }

  const conflicts = error instanceof APIError ? error.conflicts ?? [] : [];

  return (
    <div className="modal-backdrop" role="presentation">
      <section className="dialog" role="dialog" aria-modal="true" aria-labelledby="policy-import-title">
        <div className="dialog-header">
          <div><h2 id="policy-import-title">Import rule lists</h2><p>Rules are appended to this policy. Existing target assignments are unchanged.</p></div>
          <button className="icon-button" type="button" aria-label="Close import dialog" onClick={onClose}>×</button>
        </div>
        {!result ? <>
          <label className="dialog-field wide"><span>JSON file</span><input type="file" accept="application/json,.json" onChange={(event) => { const file = event.target.files?.[0]; if (file) void readFile(file); }} /></label>
          {document ? <div className="policy-import-preview"><strong>Preview</strong><span>{document.blacklist.length} blacklist rules</span><span>{document.whitelist.length} whitelist rules</span></div> : null}
          {fileError || error ? <div className="form-error compact">{fileError || error?.message}{conflicts.length > 0 ? <ul>{conflicts.map((entry) => <li key={`${entry.command_template}:${entry.reason ?? ''}`}>{entry.command_template}</li>)}</ul> : null}</div> : null}
          <div className="dialog-actions"><button className="ghost-button" type="button" onClick={onClose}>Cancel</button><button className="primary-action" type="button" disabled={!document || isSaving} onClick={() => void submit()}>{isSaving ? 'Importing…' : 'Append rules'}</button></div>
        </> : <>
          <div className="policy-import-preview"><strong>Import complete</strong><span>{result.added.length} rules added</span><span>{result.skipped.length} duplicates skipped</span><span>{result.conflicts.length} conflicts</span></div>
          <div className="dialog-actions"><button className="primary-action" type="button" onClick={onClose}>Done</button></div>
        </>}
      </section>
    </div>
  );
}
