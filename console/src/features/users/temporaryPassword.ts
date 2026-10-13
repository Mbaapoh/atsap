// The temporary password an administrator's reset sets (console-admin-
// screens D4). ResetPrincipalPassword takes the password from the caller,
// so the console generates one rather than letting a person type a weak
// one. The alphabet leaves out look-alikes (0/O, 1/l/I) because it is read
// out or typed by hand; 16 characters from 56 is about 93 bits, well
// inside the server's 8–64 code points.
export const TEMPORARY_ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789";
export const TEMPORARY_LENGTH = 16;

export function temporaryPassword(random: (n: number) => Uint8Array = (n) => crypto.getRandomValues(new Uint8Array(n))): string {
  const out: string[] = [];
  // Rejection sampling: only bytes below the largest multiple of the
  // alphabet size are used, so no character is more likely than another.
  const limit = 256 - (256 % TEMPORARY_ALPHABET.length);
  while (out.length < TEMPORARY_LENGTH) {
    for (const b of random(TEMPORARY_LENGTH)) {
      if (b < limit && out.length < TEMPORARY_LENGTH) out.push(TEMPORARY_ALPHABET[b % TEMPORARY_ALPHABET.length]!);
    }
  }
  return out.join("");
}
