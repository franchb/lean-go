"""Map Lean's Unicode positions through one explicit UTF-8 source insertion."""
from dataclasses import dataclass


@dataclass(frozen=True)
class SourceMap:
    original: bytes
    instrumented: bytes
    insertion: int
    inserted_bytes: int

    def position(self, line, column):
        """Lean lines are 1-based; columns count code points, including CR."""
        if type(line) is not int or type(column) is not int or line < 1 or column < 0:
            return None
        lines = self.instrumented.decode('utf-8').split('\n')
        if line > len(lines) or column > len(lines[line - 1]):
            return None
        prefix = '\n'.join(lines[:line - 1]) + ('\n' if line > 1 else '')
        offset = len((prefix + lines[line - 1][:column]).encode('utf-8'))
        if self.insertion <= offset < self.insertion + self.inserted_bytes:
            return None
        if offset >= self.insertion + self.inserted_bytes:
            offset -= self.inserted_bytes
        prefix = self.original[:offset].decode('utf-8')
        tail = prefix.rsplit('\n', 1)[-1]
        return {'line': prefix.count('\n') + 1, 'column': len(tail),
                'utf16_column': len(tail.encode('utf-16-le')) // 2, 'byte_offset': offset}
