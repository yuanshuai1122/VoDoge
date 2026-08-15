import { api } from "../client";

export interface PCSCReader {
  name: string;
  claimed_id?: string;
}

export interface ReadersStatus {
  /** running | missing | error */
  daemon: string;
  message: string;
  socket?: string;
  readers: PCSCReader[];
}

/** GET /api/readers —— pcscd 没起来也是 200，看 daemon / message。 */
export async function listReaders(): Promise<ReadersStatus> {
  return (await api.get<ReadersStatus>("/readers")).data;
}
