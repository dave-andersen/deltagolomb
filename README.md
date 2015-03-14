# deltagolomb
This module implements order-zero exponential golomb coding. The representation uses very few bits to represent small numbers (e.g., zero uses 1 bit; +1,+2,-1,-2 each use four), with a corresponding increase in length for larger numbers. On top of this core, it provides functions for taking an array of integers, delta-encoding them, and then compressing the residuals using exponential golomb coding.

This representation is great for compressing signals that vary slowly over time, but is not good for general compression, where encodings like Huffman are better suited.
