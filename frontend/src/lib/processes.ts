export type DeployedProcess = {
  bpmnProcessId: string
  name: string
  latestVersion: number
  versions: { version: number; key: string }[]
}

export type FileProcess = {
  bpmnProcessId: string
  name: string
  version: number
  processDefinitionKey: string
}

// Local deployments may not be indexed by Operate yet. Keep every version
// selectable even when its process is missing from the refreshed list.
export function mergeFileDeployments(list: DeployedProcess[], files: FileProcess[]): DeployedProcess[] {
  const merged = new Map(list.map(process => [process.bpmnProcessId, {
    ...process,
    versions: [...process.versions],
  }]))
  for (const file of files) {
    const process = merged.get(file.bpmnProcessId) ?? {
      bpmnProcessId: file.bpmnProcessId,
      name: file.name,
      latestVersion: file.version,
      versions: [],
    }
    if (!process.name) process.name = file.name
    process.latestVersion = Math.max(process.latestVersion, file.version)
    process.versions = [
      ...process.versions.filter(version => version.version !== file.version),
      { version: file.version, key: file.processDefinitionKey },
    ].sort((a, b) => b.version - a.version)
    merged.set(file.bpmnProcessId, process)
  }
  return [...merged.values()]
}
