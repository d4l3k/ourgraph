#!/usr/bin/env python3

# Copyright (c) Facebook, Inc. and its affiliates.
# All rights reserved.
#
# This source code is licensed under the BSD-style license found in the
# LICENSE.txt file in the root directory of this source tree.

entity_base = "data/ourgraph"


def get_torchbiggraph_config():

    config = dict(
        # I/O data
        entity_path=entity_base,
        edge_paths=[],
        checkpoint_path='model/ourgraph',

        # Graph structure
        entities={
            'user': {'num_partitions': 1},
            'doc': {'num_partitions': 1},
        },
        relations=[
            {
                'name': 'l',
                'lhs': 'user',
                'rhs': 'doc',
                'operator': 'complex_diagonal',
            },
        ],
        dynamic_relations=False,

        # Scoring model
        dimension=200,
        global_emb=True,
        comparator='dot',

        # Training
        num_epochs=50,
        num_uniform_negs=1000,
        loss_fn='softmax',
        lr=0.001,
    )

    return config
